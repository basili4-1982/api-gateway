package tlsfp

import (
	"encoding/hex"
	"net"
	"testing"
	"time"
)

// fakePacketConn отдаёт заранее заданные датаграммы.
type fakePacketConn struct {
	pkts [][]byte
	addr net.Addr
}

func (f *fakePacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	if len(f.pkts) == 0 {
		return 0, nil, net.ErrClosed
	}
	n := copy(p, f.pkts[0])
	f.pkts = f.pkts[1:]
	return n, f.addr, nil
}
func (f *fakePacketConn) WriteTo(p []byte, a net.Addr) (int, error) { return len(p), nil }
func (f *fakePacketConn) Close() error                              { return nil }
func (f *fakePacketConn) LocalAddr() net.Addr                       { return f.addr }
func (f *fakePacketConn) SetDeadline(t time.Time) error             { return nil }
func (f *fakePacketConn) SetReadDeadline(t time.Time) error         { return nil }
func (f *fakePacketConn) SetWriteDeadline(t time.Time) error        { return nil }

func TestQUICListenerCapturesInitial(t *testing.T) {
	pkt, err := hex.DecodeString(rfcClientInitial)
	if err != nil {
		t.Fatal(err)
	}
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 4), Port: 5555}
	reg := NewQUICRegistry()
	ln := NewQUICListener(&fakePacketConn{pkts: [][]byte{pkt, {0x40, 1, 2, 3}}, addr: addr}, reg, nil)

	buf := make([]byte, 1500)
	if _, _, err := ln.ReadFrom(buf); err != nil {
		t.Fatal(err)
	}
	ja4, ja3, ok := reg.Get(addr.String())
	if !ok || ja4 == "" || ja3 == "" {
		t.Fatalf("отпечаток не записан: %q %q %v", ja4, ja3, ok)
	}

	// Второй пакет — короткий заголовок, отпечатка быть не должно.
	if _, _, err := ln.ReadFrom(buf); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := reg.Get(addr.String()); ok {
		t.Fatal("не-Initial пакет не должен давать отпечаток")
	}
}
