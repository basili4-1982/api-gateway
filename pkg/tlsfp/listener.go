package tlsfp

import (
	"encoding/binary"
	"io"
	"net"
	"time"
)

// Fingerprinted — соединение, которое помнит свой отпечаток.
//
// Отпечаток считается один раз, при рукопожатии, и живёт на соединении: ни
// глобальных карт, ни мьютексов на запрос. Обработчик достаёт его из контекста,
// куда его положил ConnContext сервера.
type Fingerprinted interface {
	Fingerprint() (ja4, ja3 string)
}

// PeekListener читает ClientHello до передачи соединения TLS-серверу.
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

// Accept читает запись ClientHello, считает отпечатки и возвращает соединение,
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

	var ja4, ja3 string
	if h, err := ParseClientHello(full); err == nil {
		ja4 = h.JA4()
		ja3 = sumMD5(h.String())
	} else {
		l.onReject(err)
	}
	_ = c.SetReadDeadline(time.Time{})
	return &replayConn{Conn: c, buf: full, ja4: ja4, ja3: ja3}, nil
}

type errString string

func (e errString) Error() string { return string(e) }

const errBadRecord = errString("tlsfp: некорректная запись ClientHello")

// replayConn отдаёт сначала прочитанные байты, затем читает из соединения.
type replayConn struct {
	net.Conn
	buf []byte
	ja4 string
	ja3 string
}

func (c *replayConn) Read(p []byte) (int, error) {
	if len(c.buf) > 0 {
		n := copy(p, c.buf)
		c.buf = c.buf[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}

// Fingerprint отдаёт отпечатки, посчитанные при рукопожатии.
func (c *replayConn) Fingerprint() (string, string) { return c.ja4, c.ja3 }
