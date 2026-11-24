package tlsfp

import (
	"encoding/binary"
	"io"
	"net"
	"sync"
	"time"
)

// Registry хранит отпечатки соединений до того, как запрос дойдёт до обработчика.
// Ключ — адрес соединения (RemoteAddr до NAT), значение — JA3.
type Registry struct {
	mu      sync.RWMutex
	byAddr  map[string]string
	maxSize int
}

// NewRegistry создаёт реестр на 100 тысяч соединений: больше на одном узле
// одновременно не держим, старые вытесняются.
func NewRegistry() *Registry {
	return &Registry{byAddr: make(map[string]string, 1024), maxSize: 100_000}
}

// Set записывает отпечаток.
func (r *Registry) Set(addr, ja3 string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.byAddr) >= r.maxSize {
		// Дешёвое вытеснение: чистим половину. Точность не критична — запрос
		// приходит сразу после рукопожатия, когда запись ещё свежая.
		n := 0
		for k := range r.byAddr {
			delete(r.byAddr, k)
			if n++; n >= r.maxSize/2 {
				break
			}
		}
	}
	r.byAddr[addr] = ja3
}

// Get отдаёт отпечаток и удаляет запись: следующий запрос в том же
// keep-alive соединении уже не требует рукопожатия.
func (r *Registry) Get(addr string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.byAddr[addr]
	delete(r.byAddr, addr)
	return v
}

// PeekListener читает ClientHello до передачи соединения TLS-серверу.
type PeekListener struct {
	net.Listener
	reg      *Registry
	readTTL  time.Duration
	onReject func(error)
}

// NewListener оборачивает слушатель. onReject может быть nil.
func NewListener(l net.Listener, reg *Registry, onReject func(error)) *PeekListener {
	if reg == nil {
		reg = NewRegistry()
	}
	if onReject == nil {
		onReject = func(error) {}
	}
	return &PeekListener{Listener: l, reg: reg, readTTL: 5 * time.Second, onReject: onReject}
}

// Registry отдаёт реестр, чтобы обработчики могли читать отпечаток.
func (l *PeekListener) Registry() *Registry { return l.reg }

// Accept читает запись ClientHello, считает отпечаток и возвращает соединение,
// у которого эти байты уже «прочитаны» — TLS-сервер получит их без потерь.
func (l *PeekListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	_ = c.SetReadDeadline(time.Now().Add(l.readTTL))
	head := make([]byte, 5)
	if _, err := io.ReadFull(c, head); err != nil {
		l.onReject(err)
		_ = c.Close()
		return nil, err
	}
	recLen := int(binary.BigEndian.Uint16(head[3:5]))
	// Запись больше 64 КБ не бывает: защищаемся от мусора в первых байтах.
	if recLen <= 0 || recLen > 1<<16 {
		l.onReject(errBadRecord)
		_ = c.Close()
		return nil, errBadRecord
	}
	rest := make([]byte, recLen)
	if _, err := io.ReadFull(c, rest); err != nil {
		l.onReject(err)
		_ = c.Close()
		return nil, err
	}
	full := make([]byte, 0, len(head)+len(rest))
	full = append(full, head...)
	full = append(full, rest...)

	if sum, err := JA3(full); err == nil {
		l.reg.Set(c.RemoteAddr().String(), sum)
	} else {
		l.onReject(err)
	}
	_ = c.SetReadDeadline(time.Time{})
	return &replayConn{Conn: c, buf: full}, nil
}

type errString string

func (e errString) Error() string { return string(e) }

const errBadRecord = errString("tlsfp: некорректная запись ClientHello")

// replayConn отдаёт сначала прочитанные байты, затем читает из соединения.
type replayConn struct {
	net.Conn
	buf []byte
}

func (c *replayConn) Read(p []byte) (int, error) {
	if len(c.buf) > 0 {
		n := copy(p, c.buf)
		c.buf = c.buf[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}
