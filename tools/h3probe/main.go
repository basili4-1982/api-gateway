// h3probe — проверка HTTP/3: делает один запрос по QUIC и печатает код ответа.
package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/quic-go/quic-go/http3"
)

func main() {
	url := flag.String("url", "", "адрес для проверки")
	ua := flag.String("ua", "Mozilla/5.0 (X11; Linux x86_64) Chrome/131.0", "User-Agent")
	flag.Parse()
	if *url == "" {
		log.Fatal("нужен -url")
	}

	rt := &http3.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // проверка стенда
	defer func() { _ = rt.Close() }()

	client := &http.Client{Transport: rt, Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", *url, nil)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("User-Agent", *ua)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "ru-RU")

	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("запрос: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
	fmt.Printf("proto=%s status=%d body=%s\n", resp.Proto, resp.StatusCode, body)
}
