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

func TestMerge_PoolsRulesForSameRouteDifferentTargets(t *testing.T) {
	// Один и тот же route (host, path_prefix, methods), но разные таргеты:
	// оба правила сохраняются — из них соберётся пул балансировки.
	got := Merge(baseCfg(),
		[]TargetConfig{{Name: "svc", URL: "http://svc:9000"}},
		[]RoutingRule{{Host: "", PathPrefix: "/api", TargetName: "svc"}},
	)
	if len(got.Routing.Rules) != 2 {
		t.Fatalf("pooled rule must be kept, got %d rules", len(got.Routing.Rules))
	}
	if got.Routing.Rules[0].TargetName != "static-api" {
		t.Errorf("static rule must come first, got %s", got.Routing.Rules[0].TargetName)
	}
	if got.Routing.Rules[1].TargetName != "svc" {
		t.Errorf("discovered pooled rule must be kept, got %s", got.Routing.Rules[1].TargetName)
	}
}

func TestMerge_DedupsIdenticalRule(t *testing.T) {
	// Полностью одинаковые route и target_name дедуплицируются.
	got := Merge(baseCfg(),
		[]TargetConfig{{Name: "svc", URL: "http://svc:9000"}},
		[]RoutingRule{
			{Host: "", PathPrefix: "/api", TargetName: "svc"},
			{Host: "", PathPrefix: "/api", TargetName: "svc"},
		},
	)
	if len(got.Routing.Rules) != 2 {
		t.Fatalf("duplicate rule must be skipped, got %d rules", len(got.Routing.Rules))
	}
}

func TestMerge_KeepsSameTargetDifferentMethods(t *testing.T) {
	// Методы входят в ключ route: GET и POST — разные пулы.
	got := Merge(baseCfg(),
		[]TargetConfig{{Name: "svc", URL: "http://svc:9000"}},
		[]RoutingRule{
			{Host: "", PathPrefix: "/api", TargetName: "svc", Methods: []string{"GET"}},
			{Host: "", PathPrefix: "/api", TargetName: "svc", Methods: []string{"POST"}},
		},
	)
	if len(got.Routing.Rules) != 3 {
		t.Fatalf("rules with different methods must both survive, got %d rules", len(got.Routing.Rules))
	}
}

func TestMerge_DefaultsDiscoveredWeight(t *testing.T) {
	got := Merge(baseCfg(),
		[]TargetConfig{{Name: "svc", URL: "http://svc:9000"}},
		nil,
	)
	if got.Targets[1].EffectiveWeight() != 1 {
		t.Errorf("discovered target weight = %d, want default 1", got.Targets[1].EffectiveWeight())
	}
}

func TestMerge_PreservesExplicitZeroWeight(t *testing.T) {
	zero := 0
	got := Merge(baseCfg(),
		[]TargetConfig{{Name: "svc", URL: "http://svc:9000", Weight: &zero}},
		nil,
	)
	if got.Targets[1].EffectiveWeight() != 0 {
		t.Errorf("explicit zero weight must survive Merge, got %d", got.Targets[1].EffectiveWeight())
	}
}

func TestMerge_DoesNotMutateBase(t *testing.T) {
	base := baseCfg()
	_ = Merge(base, []TargetConfig{{Name: "svc", URL: "http://svc:9000"}}, nil)
	if len(base.Targets) != 1 {
		t.Fatalf("base must not be mutated, got %d targets", len(base.Targets))
	}
}

func TestMerge_DropsRuleForCollidingDiscoveredTarget(t *testing.T) {
	got := Merge(baseCfg(),
		[]TargetConfig{{Name: "static-api", URL: "http://other:1"}},
		[]RoutingRule{{Host: "h", PathPrefix: "/collide", TargetName: "static-api"}},
	)
	if len(got.Routing.Rules) != 1 {
		t.Fatalf("rule of colliding discovered target must be dropped, got %d rules", len(got.Routing.Rules))
	}
	if got.Routing.Rules[0].TargetName != "static-api" {
		t.Errorf("only the static rule must remain, got %+v", got.Routing.Rules[0])
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
