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
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"time"

	"code.sarnas.ru/api-gateway/pkg/plugin"
	"code.sarnas.ru/xgate-modules/fingerprint"

	"github.com/quic-go/quic-go/http3"
)

func main() {
	dir := flag.String("dir", ".", "каталог для самоподписанного сертификата")
	mode := flag.String("mode", "both", "fp | plain | both — какие серверы поднимать")
	pprofAddr := flag.String("pprof", "127.0.0.1:6060", "адрес pprof (пусто — выключить)")
	flag.Parse()

	if *pprofAddr != "" {
		go func() {
			admin := &http.Server{Addr: *pprofAddr, Handler: http.DefaultServeMux, ReadHeaderTimeout: 5 * time.Second}
			log.Println("pprof: http://" + *pprofAddr + "/debug/pprof/")
			if err := admin.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("pprof: %v", err)
			}
		}()
	}

	cert, err := selfSigned(*dir)
	if err != nil {
		log.Fatalf("сертификат: %v", err)
	}
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}

	if *mode == "fp" || *mode == "both" {
		// pprof-ручки живут в DefaultServeMux, чтобы не раздувать основной.
	} else {
		_ = *mode
	}

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
	if *mode == "fp" || *mode == "both" {
		go func() {
			log.Println("с отпечатком: https://127.0.0.1:8443")
			if err := withFP.Serve(tls.NewListener(fp.WrapListener(lnFP), tlsCfg)); err != nil && err != http.ErrServerClosed {
				log.Fatal(err)
			}
		}()
	} else {
		_ = lnFP.Close()
	}

	lnPlain, err := net.Listen("tcp", plain.Addr)
	if err != nil {
		log.Fatalf("listen 8444: %v", err)
	}
	if *mode == "plain" || *mode == "both" {
		go func() {
			log.Println("без отпечатка: https://127.0.0.1:8444")
			if err := plain.Serve(tls.NewListener(lnPlain, tlsCfg)); err != nil && err != http.ErrServerClosed {
				log.Fatal(err)
			}
		}()
	} else {
		_ = lnPlain.Close()
	}

	// HTTP/3 без модуля — базовая линия для сравнения.
	if *mode == "plain" || *mode == "both" {
		plainH3ln, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8446})
		if err != nil {
			log.Fatalf("listen 8446/udp: %v", err)
		}
		plainTLS := tlsCfg.Clone()
		plainTLS.NextProtos = []string{"h3"}
		plainH3 := &http3.Server{TLSConfig: plainTLS, Handler: ok}
		go func() {
			log.Println("HTTP/3 без отпечатка: https://127.0.0.1:8446")
			if err := plainH3.Serve(plainH3ln); err != nil && err != http.ErrServerClosed {
				log.Printf("http3 plain: %v", err)
			}
		}()
	}

	// HTTP/3 на том же модуле: отпечаток снимается с Initial-пакета.
	if *mode == "fp" || *mode == "both" {
		h3ln, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8445})
		if err != nil {
			log.Fatalf("listen 8445/udp: %v", err)
		}
		h3TLS := tlsCfg.Clone()
		h3TLS.NextProtos = []string{"h3"}
		h3 := &http3.Server{
			TLSConfig:   h3TLS,
			Handler:     fp.Wrap(ok),
			ConnContext: fp.HTTP3ConnContext,
		}
		go func() {
			log.Println("HTTP/3 с отпечатком: https://127.0.0.1:8445")
			if err := h3.Serve(fp.WrapPacketConn(h3ln)); err != nil && err != http.ErrServerClosed {
				log.Printf("http3: %v", err)
			}
		}()
	}

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
