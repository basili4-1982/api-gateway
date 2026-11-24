// fplab — стенд для замера стоимости отпечатка под нагрузкой.
//
// Поднимает два TLS-сервера с одинаковым обработчиком:
//
//	:8443 — с обёрткой слушателя и модулем отпечатков (как в проде)
//	:8444 — без обёртки и без модуля (базовая линия)
//
// На каждом два пути: /plain (без middleware) и /fp (под middleware модуля).
// Так видно отдельно цену чтения ClientHello и цену заголовков.
//
// Запуск:
//
//	go run ./tools/fplab -dir /tmp/fplab
//	BASE=https://127.0.0.1:8443 k6 run tools/fplab/bench.js
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"code.sarnas.ru/api-gateway/pkg/plugin"
	"code.sarnas.ru/xgate-modules/fingerprint"
)

func main() {
	dir := flag.String("dir", ".", "каталог для самоподписанного сертификата")
	flag.Parse()

	cert, err := selfSigned(*dir)
	if err != nil {
		log.Fatalf("сертификат: %v", err)
	}
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}

	mux := http.NewServeMux()
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Has-FP", r.Header.Get("X-JA4"))
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/plain", ok)
	muxFP := http.NewServeMux()
	muxFP.Handle("/plain", ok)                            // только чтение ClientHello, без заголовков
	muxFP.Handle("/fp", fingerprint.NewModule().Wrap(ok)) // полный путь

	fp := fingerprint.NewModule()

	// Сервер с отпечатком: слушатель обёрнут, отпечаток в контексте соединения.
	withFP := &http.Server{
		Addr:              "127.0.0.1:8443",
		Handler:           muxFP,
		ConnContext:       fp.ConnContext,
		ReadHeaderTimeout: 5 * time.Second,
	}
	plain := &http.Server{
		Addr:              "127.0.0.1:8444",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	lnFP, err := net.Listen("tcp", withFP.Addr)
	if err != nil {
		log.Fatalf("listen 8443: %v", err)
	}
	go func() {
		log.Println("с отпечатком: https://127.0.0.1:8443")
		if err := withFP.Serve(tls.NewListener(fp.WrapListener(lnFP), tlsCfg)); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	lnPlain, err := net.Listen("tcp", plain.Addr)
	if err != nil {
		log.Fatalf("listen 8444: %v", err)
	}
	go func() {
		log.Println("без отпечатка: https://127.0.0.1:8444")
		if err := plain.Serve(tls.NewListener(lnPlain, tlsCfg)); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	select {}
}

// selfSigned создаёт самоподписанный сертификат для 127.0.0.1.
func selfSigned(dir string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "cert.pem"), certPEM, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "key.pem"), keyPEM, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}

// fpIface — методы модуля, которые нужны стенду. Проверяются на этапе компиляции.
type fpIface interface {
	plugin.Module
	Wrap(http.Handler) http.Handler
	WrapListener(net.Listener) net.Listener
	ConnContext(context.Context, net.Conn) context.Context
}

var _ fpIface = fingerprint.NewModule()
