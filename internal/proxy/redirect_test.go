package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"code.sarnas.ru/api-gateway/internal/config"
)

func TestRedirectHost(t *testing.T) {
	domains := []string{"sarnas.ru", "*.sarnas.ru"}
	cases := []struct {
		host string
		want string
	}{
		{"sarnas.ru", "sarnas.ru"},
		{"app.sarnas.ru", "app.sarnas.ru"},
		{"sarnas.ru:8080", "sarnas.ru"},
		{"SARNAS.RU", "sarnas.ru"},
		// Чужой и пустой Host не отражаются: редирект уходит на первый домен.
		{"evil.com", "sarnas.ru"},
		{"notsarnas.ru", "sarnas.ru"},
		{"", "sarnas.ru"},
	}
	for _, tc := range cases {
		if got := redirectHost(tc.host, domains); got != tc.want {
			t.Errorf("redirectHost(%q) = %q, want %q", tc.host, got, tc.want)
		}
	}

	// Только wildcard-домены: apex не матчится, fallback — базовый домен.
	if got := redirectHost("sarnas.ru", []string{"*.sarnas.ru"}); got != "sarnas.ru" {
		t.Errorf("wildcard-only fallback = %q, want sarnas.ru", got)
	}
}

// TestHTTPRedirectDoesNotReflectHost: Location редиректа собирается только из
// доменов конфигурации, иначе Host: evil.com давал open redirect.
func TestHTTPRedirectDoesNotReflectHost(t *testing.T) {
	mp := &MultiProxy{}
	mp.config.Store(&config.Config{
		TLS: &config.TLSConfig{
			Enabled:      true,
			RedirectHTTP: true,
			Domains:      []string{"sarnas.ru", "*.sarnas.ru"},
		},
	})
	handler := mp.httpRedirectHandler()

	r := httptest.NewRequest("GET", "http://evil.com/x?y=1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://sarnas.ru/x?y=1" {
		t.Fatalf("Location = %q, want https://sarnas.ru/x?y=1 (host must not be reflected)", loc)
	}

	r2 := httptest.NewRequest("GET", "http://app.sarnas.ru/x", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, r2)
	if loc := rec2.Header().Get("Location"); loc != "https://app.sarnas.ru/x" {
		t.Fatalf("Location = %q, want https://app.sarnas.ru/x", loc)
	}
}
