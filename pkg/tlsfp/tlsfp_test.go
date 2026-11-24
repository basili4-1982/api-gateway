package tlsfp

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
)

// buildHello собирает ClientHello: тест не зависит ни от сети, ни от версии Go.
func buildHello() []byte {
	var ext []byte
	addExt := func(typ uint16, data []byte) {
		var hdr [4]byte
		binary.BigEndian.PutUint16(hdr[0:2], typ)
		binary.BigEndian.PutUint16(hdr[2:4], uint16(len(data)))
		ext = append(ext, hdr[:]...)
		ext = append(ext, data...)
	}
	addExt(0, append([]byte{0x00, 0x00, 0x0b, 0x00, 0x00, 0x08}, []byte("example.")...))
	addExt(10, []byte{0x00, 0x02, 0x00, 0x1d})
	addExt(11, []byte{0x01, 0x00})

	body := []byte{0x03, 0x03}
	body = append(body, make([]byte, 32)...)
	body = append(body, 0x00)
	body = append(body, 0x00, 0x02, 0x13, 0x01)
	body = append(body, 0x01, 0x00)
	var extLen [2]byte
	binary.BigEndian.PutUint16(extLen[:], uint16(len(ext)))
	body = append(body, extLen[:]...)
	body = append(body, ext...)

	hs := append([]byte{0x01, 0x00, 0x00, 0x00}, body...)
	hs[1] = byte(len(body) >> 16)
	hs[2] = byte(len(body) >> 8)
	hs[3] = byte(len(body))

	rec := append([]byte{0x16, 0x03, 0x01, 0x00, 0x00}, hs...)
	rec[3] = byte(len(hs) >> 8)
	rec[4] = byte(len(hs))
	return rec
}

func TestParseClientHello(t *testing.T) {
	h, err := ParseClientHello(buildHello())
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if h.Version != 0x0303 {
		t.Fatalf("версия = %x", h.Version)
	}
	if len(h.Ciphers) != 1 || h.Ciphers[0] != 0x1301 {
		t.Fatalf("шифры = %v", h.Ciphers)
	}
	if len(h.Curves) != 1 || h.Curves[0] != 29 {
		t.Fatalf("кривые = %v", h.Curves)
	}
	if len(h.Extensions) != 3 {
		t.Fatalf("расширения = %v", h.Extensions)
	}
}

func TestJA3MatchesKnownString(t *testing.T) {
	want := "771,4865,0-10-11,29,0"
	h, err := ParseClientHello(buildHello())
	if err != nil {
		t.Fatal(err)
	}
	if got := h.String(); got != want {
		t.Fatalf("строка JA3 = %q, ожидалось %q", got, want)
	}
	sum, err := JA3(buildHello())
	if err != nil {
		t.Fatal(err)
	}
	if len(sum) != 32 {
		t.Fatalf("хеш = %q", sum)
	}
}

func TestRejectsGarbage(t *testing.T) {
	if _, err := ParseClientHello([]byte("GET / HTTP/1.1\r\n\r\n")); err == nil {
		t.Fatal("HTTP-запрос не должен разбираться как ClientHello")
	}
	if _, err := ParseClientHello([]byte{0x16}); err == nil {
		t.Fatal("обрезанная запись должна отвергаться")
	}
}

// pipeListener отдаёт одно соединение: так тест обходится без сети.
type pipeListener struct {
	conn net.Conn
	done chan struct{}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	if l.conn == nil {
		<-l.done
		return nil, net.ErrClosed
	}
	c := l.conn
	l.conn = nil
	return c, nil
}
func (l *pipeListener) Close() error   { return nil }
func (l *pipeListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }

func TestListenerCapturesHello(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	ln := &pipeListener{conn: serverConn, done: make(chan struct{})}
	reg := NewRegistry()
	peek := NewListener(ln, reg, nil)

	hello := buildHello()
	go func() {
		_, _ = clientConn.Write(hello)
	}()

	conn, err := peek.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}

	if got := reg.Get(serverConn.RemoteAddr().String()); got == "" {
		t.Fatal("отпечаток не записан")
	}

	// Соединение должно отдавать уже прочитанные байты: TLS-сервер не увидит дыру.
	buf := make([]byte, len(hello))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("чтение после replay: %v", err)
	}
	if string(buf) != string(hello) {
		t.Fatal("байты ClientHello потерялись при replay")
	}
}
