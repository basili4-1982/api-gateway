package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleConfig = `
server:
  port: 9090
tls:
  enabled: true
  domains: ["example.com"]
  email: "ops@example.com"
jwt:
  secret_key: "SUPER_SECRET_JWT"
basic_auth:
  enabled: true
  username: "admin"
  password: "SUPER_SECRET_PASSWORD"
permissions:
  enabled: true
  service_url: "http://perm:8080"
  api_key: "SUPER_SECRET_API_KEY"
  invalidate_token: "SUPER_SECRET_INVALIDATE"
discovery:
  enabled: true
  provider: docker
targets:
  - name: "t1"
    url: "http://localhost:9001"
    path_prefix: "/api"
routing:
  rules:
    - path_prefix: "/api"
      target_name: "t1"
webhooks:
  - name: "wh1"
    transport: "webhook"
    webhook_url: "http://hooks:9000/x"
    trigger: "on_request"
`

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigSummary(t *testing.T) {
	path := writeTempConfig(t, sampleConfig)

	c, err := LoadConfigSummary(path)
	if err != nil {
		t.Fatalf("LoadConfigSummary() error = %v", err)
	}
	if c.Port != 9090 {
		t.Errorf("Port = %d, want 9090", c.Port)
	}
	if !c.TLSEnabled {
		t.Error("TLSEnabled = false, want true")
	}
	if !c.DiscoveryEnabled {
		t.Error("DiscoveryEnabled = false, want true")
	}
	if !c.PermissionsEnabled {
		t.Error("PermissionsEnabled = false, want true")
	}
	if c.TargetCount != 1 {
		t.Errorf("TargetCount = %d, want 1", c.TargetCount)
	}
	if c.RuleCount != 1 {
		t.Errorf("RuleCount = %d, want 1", c.RuleCount)
	}
	if c.WebhookCount != 1 {
		t.Errorf("WebhookCount = %d, want 1", c.WebhookCount)
	}
	if len(c.WebhookTransports) != 1 || c.WebhookTransports[0] != "webhook" {
		t.Errorf("WebhookTransports = %v, want [webhook]", c.WebhookTransports)
	}
}

func TestLoadConfigSummaryNoSecrets(t *testing.T) {
	path := writeTempConfig(t, sampleConfig)

	c, err := LoadConfigSummary(path)
	if err != nil {
		t.Fatalf("LoadConfigSummary() error = %v", err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{
		"SUPER_SECRET_JWT",
		"SUPER_SECRET_PASSWORD",
		"SUPER_SECRET_API_KEY",
		"SUPER_SECRET_INVALIDATE",
	} {
		if strings.Contains(string(data), secret) {
			t.Errorf("summary JSON leaks secret %q: %s", secret, data)
		}
	}
}

func TestLoadConfigSummaryInvalid(t *testing.T) {
	path := writeTempConfig(t, "targets: []\n")
	if _, err := LoadConfigSummary(path); err == nil {
		t.Fatal("LoadConfigSummary() error = nil, want error for invalid config")
	}
}

func TestLoadConfigSummaryMissingFile(t *testing.T) {
	if _, err := LoadConfigSummary(filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
		t.Fatal("LoadConfigSummary() error = nil, want error for missing file")
	}
}
