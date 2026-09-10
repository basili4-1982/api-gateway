package config

import (
	"testing"
	"time"
)

func baseCfg() *Config {
	return &Config{
		Targets: []TargetConfig{
			{Name: "static-api", URL: "http://static-api:8080", Timeout: time.Second},
		},
		Routing: RoutingConfig{
			Rules: []RoutingRule{
				{Host: "", PathPrefix: "/api", TargetName: "static-api"},
			},
		},
	}
}

func TestMerge_AppendsDiscovered(t *testing.T) {
	got := Merge(baseCfg(),
		[]TargetConfig{{Name: "svc", URL: "http://svc:9000"}},
		[]RoutingRule{{Host: "svc.local", PathPrefix: "/", TargetName: "svc"}},
	)
	if len(got.Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(got.Targets))
	}
	if len(got.Routing.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(got.Routing.Rules))
	}
}

func TestMerge_StaticTargetWinsOnNameCollision(t *testing.T) {
	got := Merge(baseCfg(),
		[]TargetConfig{{Name: "static-api", URL: "http://other:1"}},
		nil,
	)
	if got.Targets[0].URL != "http://static-api:8080" {
		t.Errorf("static target must win, got %s", got.Targets[0].URL)
	}
	if len(got.Targets) != 1 {
		t.Errorf("duplicate target must be skipped, got %d", len(got.Targets))
	}
}

func TestMerge_StaticRuleWinsOnHostPathCollision(t *testing.T) {
	got := Merge(baseCfg(),
		[]TargetConfig{{Name: "svc", URL: "http://svc:9000"}},
		[]RoutingRule{{Host: "", PathPrefix: "/api", TargetName: "svc"}},
	)
	if len(got.Routing.Rules) != 1 {
		t.Fatalf("colliding rule must be skipped, got %d", len(got.Routing.Rules))
	}
	if got.Routing.Rules[0].TargetName != "static-api" {
		t.Errorf("static rule must win, got %s", got.Routing.Rules[0].TargetName)
	}
}

func TestMerge_DoesNotMutateBase(t *testing.T) {
	base := baseCfg()
	_ = Merge(base, []TargetConfig{{Name: "svc", URL: "http://svc:9000"}}, nil)
	if len(base.Targets) != 1 {
		t.Fatalf("base must not be mutated, got %d targets", len(base.Targets))
	}
}

func TestMerge_DropsRuleForUnknownTarget(t *testing.T) {
	got := Merge(baseCfg(), nil,
		[]RoutingRule{{PathPrefix: "/x", TargetName: "ghost"}},
	)
	if len(got.Routing.Rules) != 1 {
		t.Fatalf("invalid rule must be dropped, got %d rules", len(got.Routing.Rules))
	}
	if got.Routing.Rules[0].TargetName != "static-api" {
		t.Errorf("only valid static rule should remain")
	}
}
