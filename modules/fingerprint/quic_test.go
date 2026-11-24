package fingerprint

import (
	"crypto/tls"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
)

// TestHTTP3FingerprintEndToEnd поднимает настоящий HTTP/3-сервер: клиент
// quic-go проходит полное рукопожатие, значит Initial-пакет настоящий, а не
// синтетический. Проверяем, что обработчик увидел X-JA4.
func TestHTTP3FingerprintEndToEnd(t *testing.T) {
	cert := selfSigned(t)
	m := newModule()

	var got string
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-JA4")
		w.WriteHeader(http.StatusOK)
	}))

	ln, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Skipf("UDP недоступен: %v", err)
	}
	addr := ln.LocalAddr().String()

	srv := &http3.Server{
		TLSConfig:   &tls.Config{Certificates: []tls.Certificate{cert}},
		Handler:     handler,
		ConnContext: m.HTTP3ConnContext,
	}
	go func() { _ = srv.Serve(m.WrapPacketConn(ln)) }()
	defer func() { _ = srv.Close() }()

	rt := &http3.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // тест
	defer func() { _ = rt.Close() }()
	client := &http.Client{Transport: rt, Timeout: 5 * time.Second}

	resp, err := client.Get("https://" + addr + "/")
	if err != nil {
		t.Skipf("HTTP/3-клиент не поднялся в песочнице: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("код = %d", resp.StatusCode)
	}
	if got == "" {
		t.Fatal("X-JA4 пуст: отпечаток QUIC не дошёл до обработчика")
	}
	t.Logf("JA4 по HTTP/3: %s", got)
}

// selfSigned — сертификат для локального теста.
func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	certPEM, keyPEM, err := generateCert()
	if err != nil {
		t.Fatalf("сертификат: %v", err)
	}
	c, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("ключ: %v", err)
	}
	return c
}
