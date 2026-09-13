package dashboard

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleState = `{
  "Targets": [
    {"Name": "svc", "URL": "http://10.0.0.5:8080", "Timeout": 30000000000, "PathPrefix": "/api", "StripPrefix": true, "Weight": 2, "HealthCheck": "http://10.0.0.5:8080/health"}
  ],
  "Rules": [
    {"Host": "example.com", "PathPrefix": "/api", "TargetName": "svc", "Methods": ["GET", "POST"], "StripPath": false}
  ]
}`

func TestLoadDiscoverySummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(sampleState), 0o600); err != nil {
		t.Fatal(err)
	}

	d, err := LoadDiscoverySummary(path)
	if err != nil {
		t.Fatalf("LoadDiscoverySummary() error = %v", err)
	}
	if !d.Loaded {
		t.Error("Loaded = false, want true")
	}
	if len(d.Targets) != 1 {
		t.Fatalf("len(Targets) = %d, want 1", len(d.Targets))
	}
	if d.Targets[0].Name != "svc" {
		t.Errorf("Targets[0].Name = %q, want svc", d.Targets[0].Name)
	}
	if d.Targets[0].URL != "http://10.0.0.5:8080" {
		t.Errorf("Targets[0].URL = %q, want http://10.0.0.5:8080", d.Targets[0].URL)
	}
	if d.Targets[0].Weight != 2 {
		t.Errorf("Targets[0].Weight = %d, want 2", d.Targets[0].Weight)
	}
	if len(d.Rules) != 1 {
		t.Fatalf("len(Rules) = %d, want 1", len(d.Rules))
	}
	if d.Rules[0].TargetName != "svc" || d.Rules[0].Host != "example.com" {
		t.Errorf("Rules[0] = %+v, want target svc host example.com", d.Rules[0])
	}
}

func TestLoadDiscoverySummaryCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDiscoverySummary(path); err == nil {
		t.Fatal("LoadDiscoverySummary() error = nil, want error for corrupt state")
	}
}

func TestLoadDiscoverySummaryMissing(t *testing.T) {
	if _, err := LoadDiscoverySummary(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("LoadDiscoverySummary() error = nil, want error for missing file")
	}
}

func TestRedactURL(t *testing.T) {
	got := redactURL("http://user:pass@example.com:8080/path")
	if got == "" || got == "http://user:pass@example.com:8080/path" {
		t.Errorf("redactURL() = %q, want credentials stripped", got)
	}
	if got := redactURL("http://example.com:8080/path"); got != "http://example.com:8080/path" {
		t.Errorf("redactURL() = %q, want unchanged", got)
	}
}
