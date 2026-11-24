// h3load — простой генератор нагрузки по HTTP/3: N воркеров, у каждого своё
// соединение и последовательные запросы. Нужен, чтобы снять pprof под h3.
package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go/http3"
)

func main() {
	url := flag.String("url", "", "адрес")
	workers := flag.Int("w", 50, "воркеров (по соединению на воркер)")
	dur := flag.Duration("d", 30*time.Second, "длительность")
	flag.Parse()
	if *url == "" {
		log.Fatal("нужен -url")
	}

	var ok, fail atomic.Int64
	deadline := time.Now().Add(*dur)
	var wg sync.WaitGroup
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rt := &http3.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // стенд
			defer func() { _ = rt.Close() }()
			client := &http.Client{Transport: rt, Timeout: 5 * time.Second}
			for time.Now().Before(deadline) {
				resp, err := client.Get(*url)
				if err != nil {
					fail.Add(1)
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode == 200 {
					ok.Add(1)
				} else {
					fail.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	reqs := ok.Load() + fail.Load()
	fmt.Printf("requests=%d ok=%d fail=%d rps=%.0f\n", reqs, ok.Load(), fail.Load(), float64(reqs)/dur.Seconds())
}
