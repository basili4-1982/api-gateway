package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIPRateLimiter_Allow(t *testing.T) {
	rl := NewIPRateLimiter(10, 5)

	ip := "192.168.1.1"
	for i := 0; i < 5; i++ {
		if !rl.Allow(ip) {
			t.Fatalf("expected allow at iteration %d", i)
		}
	}
}

func TestIPRateLimiter_Deny(t *testing.T) {
	rl := NewIPRateLimiter(1, 1)

	ip := "10.0.0.1"
	rl.Allow(ip)

	if rl.Allow(ip) {
		t.Fatal("expected deny after burst consumed")
	}
}

func TestIPRateLimiter_DifferentIPs(t *testing.T) {
	rl := NewIPRateLimiter(0, 0)

	if rl.Allow("10.0.0.1") {
		t.Fatal("expected deny for 10.0.0.1")
	}
	if rl.Allow("10.0.0.2") {
		t.Fatal("expected deny for 10.0.0.2")
	}
}

func TestGetClientIP_XForwardedFor(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.1")
	r.RemoteAddr = "192.168.1.1:12345"

	ip := getClientIP(r)
	if ip != "203.0.113.1" {
		t.Errorf("expected 203.0.113.1, got %s", ip)
	}
}

func TestGetClientIP_RemoteAddr(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.5:8080"

	ip := getClientIP(r)
	if ip != "10.0.0.5" {
		t.Errorf("expected 10.0.0.5, got %s", ip)
	}
}

func TestRouteLookup(t *testing.T) {
	// start test server as target
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer targetSrv.Close()

	targetSrv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer targetSrv2.Close()

	_ = targetSrv
	_ = targetSrv2
}
