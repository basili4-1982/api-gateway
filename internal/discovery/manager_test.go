package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
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

// TestManager_ApplySerialized доказывает, что onUpdate не вызывается повторно,
// пока не завершился предыдущий вызов: второй apply (SetBase) не должен войти в
// колбэк, пока первый (onResult) удерживает applyMu.
func TestManager_ApplySerialized(t *testing.T) {
	base := &config.Config{
		Targets: []config.TargetConfig{{Name: "static", URL: "http://static:1"}},
	}

	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()

	var calls atomic.Int64
	m, err := NewManager(base, zap.NewNop(), func(*config.Config) {
		calls.Add(1)
		entered <- struct{}{}
		<-release
	})
	if err != nil {
		t.Fatal(err)
	}

	go m.onResult(Result{
		Targets: []config.TargetConfig{{Name: "a", URL: "http://a:1"}},
	})
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first onUpdate did not start")
	}

	secondDone := make(chan struct{})
	go func() {
		m.SetBase(&config.Config{
			Targets: []config.TargetConfig{{Name: "b", URL: "http://b:1"}},
		})
		close(secondDone)
	}()

	select {
	case <-entered:
		t.Fatal("second onUpdate started before the first finished")
	case <-time.After(50 * time.Millisecond):
	}

	unblock()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("second onUpdate did not run after the first finished")
	}
	select {
	case <-secondDone:
	case <-time.After(2 * time.Second):
		t.Fatal("second apply did not complete")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected 2 updates, got %d", got)
	}
}

// TestManager_LastJSONMatchesLastDelivered прогоняет конкурентные SetBase и
// onResult и проверяет, что onUpdate вызывается строго по одному, а lastJSON
// соответствует последнему доставленному конфигу.
func TestManager_LastJSONMatchesLastDelivered(t *testing.T) {
	base := &config.Config{
		Targets: []config.TargetConfig{{Name: "static", URL: "http://static:1"}},
	}

	var inFlight, maxInFlight atomic.Int64
	var mu sync.Mutex
	var delivered []string

	m, err := NewManager(base, zap.NewNop(), func(c *config.Config) {
		n := inFlight.Add(1)
		for {
			old := maxInFlight.Load()
			if n <= old || maxInFlight.CompareAndSwap(old, n) {
				break
			}
		}
		data, err := json.Marshal(c)
		if err == nil {
			mu.Lock()
			delivered = append(delivered, string(data))
			mu.Unlock()
		}
		inFlight.Add(-1)
	})
	if err != nil {
		t.Fatal(err)
	}

	const workers = 8
	const iters = 40
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				if j%2 == 0 {
					m.onResult(Result{
						Targets: []config.TargetConfig{{
							Name: fmt.Sprintf("svc-%d-%d", id, j),
							URL:  "http://svc:9000",
						}},
					})
					continue
				}
				m.SetBase(&config.Config{
					Targets: []config.TargetConfig{{
						Name: fmt.Sprintf("base-%d-%d", id, j),
						URL:  "http://base:1",
					}},
				})
			}
		}(i)
	}
	wg.Wait()

	if got := maxInFlight.Load(); got != 1 {
		t.Fatalf("onUpdate ran concurrently: max in-flight = %d", got)
	}

	mu.Lock()
	if len(delivered) == 0 {
		mu.Unlock()
		t.Fatal("no updates delivered")
	}
	last := delivered[len(delivered)-1]
	mu.Unlock()

	m.mu.Lock()
	lastJSON := m.lastJSON
	m.mu.Unlock()

	if lastJSON != last {
		t.Fatalf("lastJSON does not match last delivered config")
	}
}

func TestManager_StartTwiceIsNoop(t *testing.T) {
	base := &config.Config{
		Targets: []config.TargetConfig{{Name: "static", URL: "http://static:1"}},
	}
	m, err := NewManager(base, zap.NewNop(), func(*config.Config) {})
	if err != nil {
		t.Fatal(err)
	}
	p := &blockingProvider{entered: make(chan struct{})}
	m.setProvider(p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("provider did not start")
	}

	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.entered:
		t.Fatal("second Start started the provider again")
	case <-time.After(100 * time.Millisecond):
	}
	if got := p.calls.Load(); got != 1 {
		t.Fatalf("provider Start called %d times, want 1", got)
	}
	_ = m.Stop()
}

func TestManager_NilConfigReturnsError(t *testing.T) {
	m, err := NewManager(nil, zap.NewNop(), func(*config.Config) {})
	if err == nil {
		t.Fatal("expected error for nil config")
	}
	if m != nil {
		t.Fatalf("expected nil manager, got %v", m)
	}
}

type blockingProvider struct {
	calls   atomic.Int64
	entered chan struct{}
}

func (p *blockingProvider) Start(ctx context.Context, onResult func(Result)) error {
	p.calls.Add(1)
	p.entered <- struct{}{}
	<-ctx.Done()
	return nil
}

func (p *blockingProvider) Stop() error { return nil }
