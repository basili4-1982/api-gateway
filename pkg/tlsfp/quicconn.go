package tlsfp

// Обёртка UDP для HTTP/3: отпечаток снимается с Initial-пакета, сам пакет
// уходит дальше без изменений.

import (
	"net"
	"sync"
	"time"
)

// QUICRegistry хранит отпечатки по адресу клиента до первого запроса.
// Ключ — адрес: соединение QUIC одно на пару адресов до миграции.
type QUICRegistry struct {
	m sync.Map // string → quicFP
}

type quicFP struct {
	segs []cryptoSegment // накопленные куски ClientHello: он может прийти в двух пакетах
	ja4  string
	ja3  string
	at   time.Time
}

// Add складывает CRYPTO-сегменты по адресу и, если ClientHello собрался,
// считает отпечаток. Возвращает true, когда отпечаток готов.
func (r *QUICRegistry) Add(addr string, segs []cryptoSegment) bool {
	v, _ := r.m.LoadOrStore(addr, quicFP{at: time.Now()})
	f := v.(quicFP)
	f.segs = mergeSegments(f.segs, segs)
	f.at = time.Now()
	rec, err := AssembleHello(f.segs)
	if err != nil {
		r.m.Store(addr, f)
		return false
	}
	h, perr := ParseClientHello(rec)
	if perr != nil {
		r.m.Store(addr, f)
		return false
	}
	f.ja4 = h.JA4()
	f.ja3 = sumMD5(h.String())
	r.m.Store(addr, f)
	return true
}

// mergeSegments объединяет куски по смещению: повторные Initial-пакеты
// приходят при потере и не должны дублировать данные.
func mergeSegments(dst, add []cryptoSegment) []cryptoSegment {
	out := append([]cryptoSegment(nil), dst...)
	for _, s := range add {
		dup := false
		for _, e := range out {
			if e.offset == s.offset && len(e.data) == len(s.data) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, s)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].offset < out[j-1].offset; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// NewQUICRegistry создаёт реестр.
func NewQUICRegistry() *QUICRegistry { return &QUICRegistry{} }

// Set записывает отпечаток.
func (r *QUICRegistry) Set(addr, ja4, ja3 string) {
	r.m.Store(addr, quicFP{ja4: ja4, ja3: ja3, at: time.Now()})
}

// Get отдаёт отпечаток и удаляет запись: он нужен одному соединению один раз.
func (r *QUICRegistry) Get(addr string) (ja4, ja3 string, ok bool) {
	v, loaded := r.m.LoadAndDelete(addr)
	if !loaded {
		return "", "", false
	}
	f := v.(quicFP)
	if time.Since(f.at) > time.Minute {
		return "", "", false
	}
	return f.ja4, f.ja3, true
}

// QUICListener оборачивает UDP-соединение: на каждом Initial-пакете считает
// отпечаток, остальные пакеты не трогает. Разбор идёт на копии данных.
type QUICListener struct {
	net.PacketConn
	reg      *QUICRegistry
	onReject func(error)
	// parseInitial вынесен полем, чтобы тесты могли подменить разбор.
	parseInitial func([]byte) ([]byte, []cryptoSegment, error)
}

// NewQUICListener оборачивает PacketConn. onReject может быть nil.
func NewQUICListener(conn net.PacketConn, reg *QUICRegistry, onReject func(error)) *QUICListener {
	if reg == nil {
		reg = NewQUICRegistry()
	}
	if onReject == nil {
		onReject = func(error) {}
	}
	return &QUICListener{PacketConn: conn, reg: reg, onReject: onReject, parseInitial: ParseInitial}
}

// Registry отдаёт реестр, чтобы обработчики искали отпечаток по адресу.
func (l *QUICListener) Registry() *QUICRegistry { return l.reg }

// ReadFrom читает датаграмму и, если это Initial, запоминает отпечаток.
func (l *QUICListener) ReadFrom(p []byte) (int, net.Addr, error) {
	n, addr, err := l.PacketConn.ReadFrom(p)
	if err != nil || n <= 0 || addr == nil {
		return n, addr, err
	}
	// Короткий заголовок и мусор отсекаются по первому байту: Initial всегда
	// длинный (0x80) и с типом 0.
	if p[0]&0x80 == 0 || (p[0]>>4)&0x03 != 0 {
		return n, addr, nil
	}
	_, segs, perr := l.parseInitial(p[:n])
	if perr != nil {
		l.onReject(perr)
		return n, addr, nil
	}
	// ClientHello может занимать несколько пакетов: складываем куски и
	// пытаемся собрать на каждом. Готовый отпечаток заберёт ConnContext.
	if !l.reg.Add(addr.String(), segs) {
		return n, addr, nil
	}
	return n, addr, nil
}
