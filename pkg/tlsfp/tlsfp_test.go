package tlsfp

import (
	"encoding/binary"
	"io"
	"net"
	"strings"
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
	addExt(0, []byte{0x00, 0x0b, 0x00, 0x00, 0x08, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.'})
	addExt(16, []byte{0x00, 0x03, 0x02, 'h', '2'})         // ALPN: h2
	addExt(43, []byte{0x02, 0x03, 0x04})                   // supported_versions: TLS 1.3
	addExt(10, []byte{0x00, 0x04, 0x1a, 0x1a, 0x00, 0x1d}) // GREASE + x25519
	addExt(11, []byte{0x01, 0x00})

	body := []byte{0x03, 0x03}
	body = append(body, make([]byte, 32)...)
	body = append(body, 0x00)
	body = append(body, 0x00, 0x04, 0x0a, 0x0a, 0x13, 0x01) // GREASE + TLS_AES_128_GCM
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
	if len(h.Ciphers) != 2 {
		t.Fatalf("шифры = %v", h.Ciphers)
	}
	if len(h.Curves) != 2 {
		t.Fatalf("кривые = %v", h.Curves)
	}
	if len(h.Extensions) != 5 {
		t.Fatalf("расширения = %v", h.Extensions)
	}
	if len(h.ALPN) != 1 || h.ALPN[0] != "h2" {
		t.Fatalf("ALPN = %v", h.ALPN)
	}
}

func TestJA3MatchesKnownString(t *testing.T) {
	want := "771,2570-4865,0-16-43-10-11,6682-29,0"
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
	peek := NewListener(ln, nil)

	hello := buildHello()
	go func() {
		_, _ = clientConn.Write(hello)
	}()

	conn, err := peek.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}

	fp, ok := conn.(Fingerprinted)
	if !ok {
		t.Fatal("соединение не отдаёт отпечаток")
	}
	ja4, ja3 := fp.Fingerprint()
	if ja4 == "" || ja3 == "" {
		t.Fatalf("отпечатки пусты: %q %q", ja4, ja3)
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

func TestJA4Prefix(t *testing.T) {
	h, err := ParseClientHello(buildHello())
	if err != nil {
		t.Fatal(err)
	}
	got := h.JA4()
	// t + 13 + d (SNI есть) + 1 шифр (GREASE вырезан) + 5 расширений + h2
	wantPrefix := "t13d15h2_"
	if len(got) < len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("префикс JA4 = %q, ожидался %q", got, wantPrefix)
	}
	parts := strings.Split(got, "_")
	if len(parts) != 3 {
		t.Fatalf("формат JA4 = %q", got)
	}
	for _, p := range parts[1:] {
		if len(p) != 12 {
			t.Fatalf("хеш в JA4 = %q, ожидались 12 символов", p)
		}
	}
}

func TestJA4IgnoresGREASE(t *testing.T) {
	withGREASE, err := ParseClientHello(buildHello())
	if err != nil {
		t.Fatal(err)
	}
	clean := withGREASE
	clean.Ciphers = []uint16{0x1301}
	clean.Curves = []uint16{0x1d}
	if withGREASE.JA4() != clean.JA4() {
		t.Fatal("GREASE не должен влиять на JA4")
	}
}
