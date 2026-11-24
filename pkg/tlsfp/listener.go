package tlsfp

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Fingerprinted — соединение, которое умеет отдать отпечаток.
//
// Значения появляются после первого чтения ClientHello: TLS-сервер читает его
// сам в своей горутине, и к моменту, когда запрос дошёл до обработчика, они уже
// готовы. Метод безопасен для параллельных вызовов.
type Fingerprinted interface {
	Fingerprint() (ja4, ja3 string)
}

// PeekListener оборачивает слушатель, но ничего не читает в Accept: иначе
// accept-loop блокируется до первого байта клиента. Разбор идёт лениво, при
// первом Read, который делает уже TLS-сервер в отдельной горутине.
type PeekListener struct {
	net.Listener
	readTTL  time.Duration
	onReject func(error)
}

// NewListener оборачивает слушатель. onReject может быть nil.
func NewListener(l net.Listener, onReject func(error)) *PeekListener {
	if onReject == nil {
		onReject = func(error) {}
	}
	return &PeekListener{Listener: l, readTTL: 5 * time.Second, onReject: onReject}
}

// Accept отдаёт соединение сразу, без чтения.
func (l *PeekListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &peekConn{Conn: c, ttl: l.readTTL, onReject: l.onReject}, nil
}

// peekConn при первом Read забирает ClientHello целиком, считает отпечаток и
// отдаёт байты дальше без потерь.
type peekConn struct {
	net.Conn
	ttl      time.Duration
	onReject func(error)

	done     atomic.Bool
	once     sync.Once
	buf      []byte
	ja4      string
	ja3      string
	parseErr error
}

// ensure разбирает ClientHello ровно один раз.
func (c *peekConn) ensure() {
	c.once.Do(func() {
		defer c.done.Store(true)
		_ = c.Conn.SetReadDeadline(time.Now().Add(c.ttl))
		defer func() { _ = c.Conn.SetReadDeadline(time.Time{}) }()

		head := make([]byte, 5)
		if _, err := io.ReadFull(c.Conn, head); err != nil {
			c.parseErr = err
			c.onReject(err)
			return
		}
		recLen := int(binary.BigEndian.Uint16(head[3:5]))
		// Запись больше 64 КБ не бывает: защищаемся от мусора в первых байтах.
		if recLen <= 0 || recLen > 1<<16 {
			c.parseErr = errBadRecord
			c.onReject(errBadRecord)
			return
		}
		rest := make([]byte, recLen)
		if _, err := io.ReadFull(c.Conn, rest); err != nil {
			c.parseErr = err
			c.onReject(err)
			return
		}
		c.buf = make([]byte, 0, 5+recLen)
		c.buf = append(c.buf, head...)
		c.buf = append(c.buf, rest...)

		h, err := ParseClientHello(c.buf)
		if err != nil {
			c.parseErr = err
			c.onReject(err)
			return
		}
		c.ja4 = h.JA4()
		c.ja3 = sumMD5(h.String())
	})
}

// Read отдаёт сначала прочитанные байты ClientHello, затем читает из соединения.
func (c *peekConn) Read(p []byte) (int, error) {
	if !c.done.Load() {
		c.ensure()
	}
	if len(c.buf) > 0 {
		n := copy(p, c.buf)
		c.buf = c.buf[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}

// Fingerprint отдаёт отпечатки. К моменту вызова рукопожатие уже прочитало
// ClientHello, поэтому ensure не блокируется; если вызвать раньше — дождётся.
func (c *peekConn) Fingerprint() (string, string) {
	c.ensure()
	return c.ja4, c.ja3
}

type errString string

func (e errString) Error() string { return string(e) }

const errBadRecord = errString("tlsfp: некорректная запись ClientHello")

// ErrParse — ClientHello не разобран; отпечатка нет.
var ErrParse = errors.New("tlsfp: отпечаток недоступен")
