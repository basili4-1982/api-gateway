package proxy

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestMetricsHandler_MultipleCallsDoNotPanic проверяет, что /metrics можно
// запрашивать многократно: раньше Handler() регистрировал expvar-переменную
// на каждый вызов и паниковал на втором запросе.
func TestMetricsHandler_MultipleCallsDoNotPanic(t *testing.T) {
	m := NewMetrics(true)
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
		if rec.Code != 200 {
			t.Fatalf("call %d: status %d, want 200", i, rec.Code)
		}
	}

	m.IncRequests("GET", "/api/blog/posts", "200")
	m.ObserveDuration("GET", "/api/blog/posts", "200", 25*time.Millisecond)
	m.SetTargetUp("blog", true)
	m.IncRateLimitDenial()

	// Кардинальность expvar ограничена нормализованным префиксом: сырые пути
	// (например, уникальные 404 от сканеров) не должны становиться ключами.
	m.IncRequests("GET", "/api/blog/posts/42", "404")
	m.IncRequests("GET", "/api/blog/posts/43", "404")
	if v := m.requestsTotal.Get("GET:/api/blog:404"); v == nil || v.String() != "2" {
		t.Fatalf("expvar key GET:/api/blog:404 = %v, want 2 (raw paths must fold into the prefix)", v)
	}
	if v := m.requestsTotal.Get("GET:/api/blog/posts/42:404"); v != nil {
		t.Fatalf("raw path became an expvar key: %v", v)
	}

	rec := httptest.NewRecorder()
	m.PromHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics/prometheus", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`gateway_requests_total{method="GET",route="/api/blog",status="200"} 1`,
		`gateway_request_duration_seconds_bucket{method="GET",route="/api/blog",status="200",le="0.025"} 1`,
		`gateway_target_up{target="blog"} 1`,
		`gateway_rate_limit_denials_total 1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
}
