package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"code.sarnas.ru/api-gateway/pkg/tlsfp"
)

func TestJA3HeaderIsScrubbedWhenUnknown(t *testing.T) {
	mp := &MultiProxy{}
	h := ja3HeaderMiddleware(mp)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(headerJA3); got != "" {
			t.Fatalf("заголовок клиента не вырезан: %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/x", nil)
	req.RemoteAddr = "10.0.0.1:5555"
	req.Header.Set(headerJA3, "deadbeef")
	req.Header.Set(headerJA3Source, "client")
	h.ServeHTTP(httptest.NewRecorder(), req)
}

func TestJA3HeaderFromRegistry(t *testing.T) {
	reg := tlsfp.NewRegistry()
	reg.Set("10.0.0.2:6666", "abc123")

	mp := &MultiProxy{}
	mp.withRegistry(reg)

	var seen, source string
	h := ja3HeaderMiddleware(mp)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get(headerJA3)
		source = r.Header.Get(headerJA3Source)
	}))

	req := httptest.NewRequest("GET", "/api/x", nil)
	req.RemoteAddr = "10.0.0.2:6666"
	h.ServeHTTP(httptest.NewRecorder(), req)

	if seen != "abc123" {
		t.Fatalf("отпечаток = %q", seen)
	}
	if source != ja3SourceGateway {
		t.Fatalf("источник = %q", source)
	}
}

func TestJA3HeaderAbsentOnKeepAlive(t *testing.T) {
	reg := tlsfp.NewRegistry()
	reg.Set("10.0.0.3:7777", "abc123")

	mp := &MultiProxy{}
	mp.withRegistry(reg)

	var got []string
	h := ja3HeaderMiddleware(mp)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get(headerJA3))
	}))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/x", nil)
		req.RemoteAddr = "10.0.0.3:7777"
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	if len(got) != 2 || got[0] != "abc123" || got[1] != "" {
		t.Fatalf("ожидалось [abc123, \"\"], получено %v", got)
	}
}
