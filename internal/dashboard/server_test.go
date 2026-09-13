package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHealthz(t *testing.T) {
	s := NewServer(ServerConfig{})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ok\n" {
		t.Errorf("body = %q, want ok\\n", rec.Body.String())
	}
}

func TestStatusEndpoint(t *testing.T) {
	cfgPath := writeTempConfig(t, sampleConfig)
	s := NewServer(ServerConfig{ConfigPath: cfgPath})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var status Status
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, rec.Body.String())
	}
	if status.Config == nil {
		t.Fatal("Config = nil, want summary")
	}
	if status.Config.Port != 9090 {
		t.Errorf("Config.Port = %d, want 9090", status.Config.Port)
	}
	if strings.Contains(rec.Body.String(), `"errors": null`) {
		t.Error("errors must be an empty array, not null")
	}
}

func TestOverviewHTML(t *testing.T) {
	cfgPath := writeTempConfig(t, sampleConfig)
	s := NewServer(ServerConfig{ConfigPath: cfgPath, Refresh: 5 * time.Second})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"API Gateway Dashboard", "9090", "http-equiv=\"refresh\""} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestOverviewUnknownPath(t *testing.T) {
	s := NewServer(ServerConfig{})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestBasicAuth(t *testing.T) {
	s := NewServer(ServerConfig{BasicAuth: "admin:secret"})
	h := s.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no creds: status = %d, want 401", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("missing WWW-Authenticate header")
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.SetBasicAuth("admin", "wrong")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong creds: status = %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.SetBasicAuth("admin", "secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct creds: status = %d, want 200", rec.Code)
	}
}

func TestGracefulDegradation(t *testing.T) {
	s := NewServer(ServerConfig{
		ConfigPath:     filepath.Join(t.TempDir(), "missing.yaml"),
		DiscoveryState: filepath.Join(t.TempDir(), "missing.json"),
		MetricsURL:     "http://127.0.0.1:1/metrics",
	})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var status Status
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if status.Config != nil || status.Discovery != nil || status.Metrics != nil {
		t.Errorf("expected all panels nil, got %+v", status)
	}
	if len(status.Errors) != 3 {
		t.Errorf("len(Errors) = %d, want 3: %v", len(status.Errors), status.Errors)
	}

	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("overview status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Ошибки") {
		t.Error("overview should render an error banner")
	}
}

func TestDisabledSources(t *testing.T) {
	s := NewServer(ServerConfig{})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	var status Status
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(status.Errors) != 0 {
		t.Errorf("len(Errors) = %d, want 0: %v", len(status.Errors), status.Errors)
	}
}

func TestStatusEndpointNoSecrets(t *testing.T) {
	cfgPath := writeTempConfig(t, sampleConfig)
	s := NewServer(ServerConfig{ConfigPath: cfgPath})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	for _, secret := range []string{
		"SUPER_SECRET_JWT",
		"SUPER_SECRET_PASSWORD",
		"SUPER_SECRET_API_KEY",
		"SUPER_SECRET_INVALIDATE",
	} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("response leaks secret %q", secret)
		}
	}
}

func TestMetricsPanel(t *testing.T) {
	metricsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleMetrics))
	}))
	defer metricsSrv.Close()

	s := NewServer(ServerConfig{MetricsURL: metricsSrv.URL})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	var status Status
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if status.Metrics == nil {
		t.Fatal("Metrics = nil, want summary")
	}
	if status.Metrics.ActiveRequests != 3 {
		t.Errorf("ActiveRequests = %d, want 3", status.Metrics.ActiveRequests)
	}
}
