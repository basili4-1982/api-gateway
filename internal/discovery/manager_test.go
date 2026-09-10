package discovery

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/basili4-1982/api-gateway/internal/config"
	"go.uber.org/zap"
)

func TestManager_MergesBaseAndDiscovered(t *testing.T) {
	// Discovery не включаем: провайдер подменяется через setProvider, чтобы
	// тест не зависел от реального Docker-хоста.
	base := &config.Config{
		Targets: []config.TargetConfig{{Name: "static", URL: "http://static:1"}},
	}

	updates := make(chan *config.Config, 10)
	m, err := NewManager(base, zap.NewNop(), func(c *config.Config) { updates <- c })
	if err != nil {
		t.Fatal(err)
	}
	m.setProvider(stubProvider{result: Result{
		Targets: []config.TargetConfig{{Name: "svc", URL: "http://svc:9000"}},
		Rules:   []config.RoutingRule{{PathPrefix: "/api/svc", TargetName: "svc"}},
	}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-updates:
		if len(got.Targets) != 2 || len(got.Routing.Rules) != 1 {
			t.Fatalf("unexpected merged config: targets=%d rules=%d", len(got.Targets), len(got.Routing.Rules))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for update")
	}
	_ = m.Stop()
}

func TestManager_NoDuplicateUpdateForSameResult(t *testing.T) {
	// base валиден (есть target), иначе apply отбросит конфиг на валидации.
	base := &config.Config{
		Targets: []config.TargetConfig{{Name: "static", URL: "http://static:1"}},
	}
	updates := atomic.Int64{}
	m, _ := NewManager(base, zap.NewNop(), func(*config.Config) { updates.Add(1) })
	m.setProvider(repeatProvider{result: Result{}, times: 3})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = m.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	if got := updates.Load(); got != 1 {
		t.Errorf("expected exactly 1 update for identical results, got %d", got)
	}
	_ = m.Stop()
}

type stubProvider struct {
	result Result
}

func (s stubProvider) Start(ctx context.Context, onResult func(Result)) error {
	onResult(s.result)
	<-ctx.Done()
	return nil
}

func (s stubProvider) Stop() error { return nil }

type repeatProvider struct {
	result Result
	times  int
}

func (s repeatProvider) Start(ctx context.Context, onResult func(Result)) error {
	for i := 0; i < s.times; i++ {
		onResult(s.result)
	}
	<-ctx.Done()
	return nil
}

func (s repeatProvider) Stop() error { return nil }
