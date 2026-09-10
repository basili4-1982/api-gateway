package discovery

import (
	"testing"
	"time"
)

func testOpts() ParseOptions {
	return ParseOptions{
		LabelPrefix:       "gateway",
		ServiceNameLabels: []string{"com.docker.compose.service"},
		DefaultTimeout:    30 * time.Second,
	}
}

func TestParseTarget_Defaults(t *testing.T) {
	res := ParseContainers([]Container{{
		ID:     "c1",
		Names:  []string{"/sarnas-blog-1"},
		Labels: map[string]string{"gateway.enable": "true", "gateway.port": "8085", "gateway.path_prefix": "/api/blog"},
	}}, testOpts())

	if len(res.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(res.Targets))
	}
	got := res.Targets[0]
	if got.Name != "sarnas-blog-1" {
		t.Errorf("name: got %q", got.Name)
	}
	if got.URL != "http://sarnas-blog-1:8085" {
		t.Errorf("url: got %q", got.URL)
	}
	if got.Timeout != 30*time.Second {
		t.Errorf("timeout: got %v", got.Timeout)
	}
}

func TestParseTarget_UsesServiceNameLabel(t *testing.T) {
	res := ParseContainers([]Container{{
		ID:    "c1",
		Names: []string{"/random-name-1"},
		Labels: map[string]string{
			"gateway.enable":             "true",
			"gateway.port":               "8085",
			"gateway.path_prefix":        "/api/blog",
			"com.docker.compose.service": "blog",
		},
	}}, testOpts())

	if len(res.Targets) != 1 || res.Targets[0].Name != "blog" {
		t.Fatalf("expected name blog, got %+v", res.Targets)
	}
	if res.Targets[0].URL != "http://blog:8085" {
		t.Errorf("url: got %q", res.Targets[0].URL)
	}
}

func TestParseTarget_ExplicitNameSchemeTimeoutHealth(t *testing.T) {
	res := ParseContainers([]Container{{
		ID: "c1",
		Labels: map[string]string{
			"gateway.enable":      "true",
			"gateway.name":        "passport",
			"gateway.port":        "8085",
			"gateway.scheme":      "https",
			"gateway.timeout":     "10s",
			"gateway.health":      "/health",
			"gateway.path_prefix": "/api/auth",
		},
	}}, testOpts())

	if len(res.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(res.Targets))
	}
	got := res.Targets[0]
	if got.Name != "passport" || got.URL != "https://passport:8085" {
		t.Errorf("name/url: got %q %q", got.Name, got.URL)
	}
	if got.Timeout != 10*time.Second {
		t.Errorf("timeout: got %v", got.Timeout)
	}
	if got.HealthCheck != "https://passport:8085/health" {
		t.Errorf("health: got %q", got.HealthCheck)
	}
}

func TestParseTarget_FullHealthURLKept(t *testing.T) {
	res := ParseContainers([]Container{{
		ID: "c1",
		Labels: map[string]string{
			"gateway.enable": "true",
			"gateway.name":   "svc",
			"gateway.port":   "9000",
			"gateway.health": "http://probe:1234/healthz",
			"gateway.host":   "svc.local",
		},
	}}, testOpts())

	if res.Targets[0].HealthCheck != "http://probe:1234/healthz" {
		t.Errorf("health: got %q", res.Targets[0].HealthCheck)
	}
}

func TestParseTarget_SingleExposedPort(t *testing.T) {
	res := ParseContainers([]Container{{
		ID:     "c1",
		Labels: map[string]string{"gateway.enable": "true", "gateway.name": "svc", "gateway.host": "x"},
		Ports:  []Port{{PrivatePort: 9000, Type: "tcp"}},
	}}, testOpts())

	if len(res.Targets) != 1 || res.Targets[0].URL != "http://svc:9000" {
		t.Fatalf("expected port 9000, got %+v", res.Targets)
	}
}

func TestParseTarget_MultiplePortsSkipped(t *testing.T) {
	res := ParseContainers([]Container{{
		ID:     "c1",
		Labels: map[string]string{"gateway.enable": "true", "gateway.name": "svc", "gateway.host": "x"},
		Ports:  []Port{{PrivatePort: 9000}, {PrivatePort: 9001}},
	}}, testOpts())

	if len(res.Targets) != 0 {
		t.Fatalf("expected skip, got %+v", res.Targets)
	}
}

func TestParseTarget_NotEnabledIgnored(t *testing.T) {
	res := ParseContainers([]Container{{
		ID:     "c1",
		Labels: map[string]string{"gateway.port": "9000"},
	}}, testOpts())

	if len(res.Targets) != 0 {
		t.Fatalf("expected no targets, got %+v", res.Targets)
	}
}

func TestParseTarget_ReplicasDeduped(t *testing.T) {
	res := ParseContainers([]Container{
		{ID: "c1", Labels: map[string]string{"gateway.enable": "true", "gateway.name": "svc", "gateway.port": "9000", "gateway.host": "x"}},
		{ID: "c2", Labels: map[string]string{"gateway.enable": "true", "gateway.name": "svc", "gateway.port": "9000", "gateway.host": "x"}},
	}, testOpts())

	if len(res.Targets) != 1 {
		t.Fatalf("expected replicas deduped to 1, got %d", len(res.Targets))
	}
	if len(res.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(res.Rules))
	}
}

func TestParseTarget_NetworkFilter(t *testing.T) {
	opts := testOpts()
	opts.Network = "backend"
	res := ParseContainers([]Container{{
		ID:       "c1",
		Labels:   map[string]string{"gateway.enable": "true", "gateway.name": "svc", "gateway.port": "9000", "gateway.host": "x"},
		Networks: map[string]Network{"frontend": {}},
	}}, opts)

	if len(res.Targets) != 0 {
		t.Fatalf("container outside network must be skipped, got %+v", res.Targets)
	}
}

func TestParseTarget_Weight(t *testing.T) {
	res := ParseContainers([]Container{{
		ID: "c1",
		Labels: map[string]string{
			"gateway.enable": "true", "gateway.name": "svc", "gateway.port": "9000",
			"gateway.host": "x", "gateway.weight": "5",
		},
	}}, testOpts())

	if len(res.Targets) != 1 || res.Targets[0].Weight != 5 {
		t.Fatalf("expected weight 5, got %+v", res.Targets)
	}
}

func TestParseTarget_NoRouteSkipped(t *testing.T) {
	res := ParseContainers([]Container{{
		ID:     "c1",
		Labels: map[string]string{"gateway.enable": "true", "gateway.name": "svc", "gateway.port": "9000"},
	}}, testOpts())

	if len(res.Targets) != 0 {
		t.Fatalf("target without any route must be skipped, got %+v", res.Targets)
	}
}
