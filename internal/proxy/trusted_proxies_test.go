package proxy

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"code.sarnas.ru/api-gateway/internal/config"
	"go.uber.org/zap"
)

func mustCIDRs(t *testing.T, values ...string) []*net.IPNet {
	t.Helper()
	nets, err := config.ParseTrustedProxies(values)
	if err != nil {
		t.Fatal(err)
	}
	return nets
}

func TestGetClientIP_IgnoresXFFWithoutTrustedProxies(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.1")
	r.RemoteAddr = "192.168.1.1:12345"

	if ip := getClientIP(r, nil); ip != "192.168.1.1" {
		t.Fatalf("ip = %q, want RemoteAddr when no trusted proxies are configured", ip)
	}
}

func TestGetClientIP_UsesXFFFromTrustedProxy(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.1")
	r.RemoteAddr = "10.0.0.5:12345"

	if ip := getClientIP(r, mustCIDRs(t, "10.0.0.0/8")); ip != "203.0.113.1" {
		t.Fatalf("ip = %q, want 203.0.113.1", ip)
	}
}

func TestGetClientIP_WalksTrustedChainFromTheRight(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 192.168.1.1, 10.0.0.5")
	r.RemoteAddr = "10.0.0.5:12345"

	if ip := getClientIP(r, mustCIDRs(t, "10.0.0.0/8", "192.168.0.0/16")); ip != "203.0.113.7" {
		t.Fatalf("ip = %q, want first untrusted address 203.0.113.7", ip)
	}
}

func TestGetClientIP_UntrustedRemoteIgnoresXFF(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.1")
	r.RemoteAddr = "198.51.100.5:12345"

	if ip := getClientIP(r, mustCIDRs(t, "10.0.0.0/8")); ip != "198.51.100.5" {
		t.Fatalf("ip = %q, want untrusted RemoteAddr", ip)
	}
}

func TestGetClientIP_ExactIPTrustedProxy(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.1")
	r.RemoteAddr = "10.0.0.5:12345"

	if ip := getClientIP(r, mustCIDRs(t, "10.0.0.5")); ip != "203.0.113.1" {
		t.Fatalf("ip = %q, want 203.0.113.1", ip)
	}
}

func TestSetForwardedFor_ReplacesXFFFromUntrustedPeer(t *testing.T) {
	mp := &MultiProxy{trustedProxies: mustCIDRs(t, "10.0.0.0/8")}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.1")
	r.RemoteAddr = "198.51.100.5:12345"

	mp.setForwardedFor(r)

	if got := r.Header.Get("X-Forwarded-For"); got != "" {
		t.Fatalf("XFF = %q, want it dropped (ReverseProxy will set the peer address)", got)
	}
}

func TestSetForwardedFor_KeepsChainFromTrustedPeer(t *testing.T) {
	mp := &MultiProxy{trustedProxies: mustCIDRs(t, "10.0.0.0/8")}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.1")
	r.RemoteAddr = "10.0.0.5:12345"

	mp.setForwardedFor(r)

	if got := r.Header.Get("X-Forwarded-For"); got != "203.0.113.1" {
		t.Fatalf("XFF = %q, want the trusted chain untouched", got)
	}
}

// TestProxyRewritesSpoofedXFFForBackend: бэкенд не должен видеть
// клиентский X-Forwarded-For, когда непосредственный пир не из
// server.trusted_proxies.
func TestProxyRewritesSpoofedXFFForBackend(t *testing.T) {
	seen := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("X-Forwarded-For")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	cfg := &config.Config{
		Targets: []config.TargetConfig{{Name: "t", URL: upstream.URL}},
		Routing: config.RoutingConfig{Rules: []config.RoutingRule{{PathPrefix: "/", TargetName: "t"}}},
	}
	mp := &MultiProxy{
		targets:     map[string]*TargetProxy{},
		routeByRule: map[*config.RoutingRule]*RouteConfig{},
		logger:      zap.NewNop(),
		metrics:     NewMetrics(false),
	}
	mp.config.Store(cfg)
	tp, err := mp.createTargetProxy(&cfg.Targets[0], false)
	if err != nil {
		t.Fatal(err)
	}
	mp.targets["t"] = tp
	mp.rebuildRouteConfigs(cfg)

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "198.51.100.5:12345"
	r.Header.Set("X-Forwarded-For", "203.0.113.1")
	rec := httptest.NewRecorder()
	mp.proxyHandler().ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := <-seen; got != "198.51.100.5" {
		t.Fatalf("backend saw XFF %q, want peer address only", got)
	}
}

func TestMetricsEndpointIgnoresSpoofedXFF(t *testing.T) {
	metrics := NewMetrics(false)
	handler := metricsEndpointMiddleware(metrics, []string{"10.0.0.1"}, func(r *http.Request) string {
		return getClientIP(r, nil)
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("GET", "/metrics", nil)
	r.RemoteAddr = "198.51.100.5:12345"
	r.Header.Set("X-Forwarded-For", "10.0.0.1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; spoofed XFF must not satisfy the metrics allowlist", rec.Code)
	}
}
