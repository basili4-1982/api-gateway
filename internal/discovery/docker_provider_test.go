package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestDockerProvider_InitialSyncAndEventResync(t *testing.T) {
	var mu sync.Mutex
	listCalls := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1.41/containers/json":
			mu.Lock()
			listCalls++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{
				"Id":"abc","Names":["/svc-1"],
				"Labels":{"gateway.enable":"true","gateway.name":"svc","gateway.port":"9000","gateway.path_prefix":"/api/svc"},
				"Ports":[{"PrivatePort":9000,"Type":"tcp"}]
			}]`))
		case r.URL.Path == "/v1.41/events":
			w.Header().Set("Content-Type", "application/json")
			flusher, _ := w.(http.Flusher)
			_, _ = w.Write([]byte(`{"Type":"container","Action":"start","id":"abc"}` + "\n"))
			if flusher != nil {
				flusher.Flush()
			}
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := newDockerProvider(srv.URL, "v1.41",
		ParseOptions{LabelPrefix: "gateway", DefaultTimeout: 30 * time.Second},
		50*time.Millisecond, time.Hour, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	results := make(chan Result, 10)
	go func() { _ = p.Start(ctx, func(r Result) { results <- r }) }()

	select {
	case r := <-results:
		if len(r.Targets) != 1 || r.Targets[0].Name != "svc" {
			t.Fatalf("unexpected initial result: %+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial sync")
	}

	select {
	case <-results:
		// событие start вызвало повторный ре-синк
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event-triggered resync")
	}

	mu.Lock()
	calls := listCalls
	mu.Unlock()
	if calls < 2 {
		t.Errorf("expected at least 2 list calls, got %d", calls)
	}
	_ = p.Stop()
}

func TestDockerProvider_ResyncsOnEventsStreamClose(t *testing.T) {
	var mu sync.Mutex
	listCalls := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1.41/containers/json":
			mu.Lock()
			listCalls++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{
				"Id":"abc","Names":["/svc-1"],
				"Labels":{"gateway.enable":"true","gateway.name":"svc","gateway.port":"9000","gateway.path_prefix":"/api/svc"},
				"Ports":[{"PrivatePort":9000,"Type":"tcp"}]
			}]`))
		case r.URL.Path == "/v1.41/events":
			// Поток событий закрывается сразу без событий — обрыв должен
			// трактоваться как reconnect-and-resync.
			w.Header().Set("Content-Type", "application/json")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := newDockerProvider(srv.URL, "v1.41",
		ParseOptions{LabelPrefix: "gateway", DefaultTimeout: 30 * time.Second},
		20*time.Millisecond, time.Hour, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	results := make(chan Result, 10)
	go func() { _ = p.Start(ctx, func(r Result) { results <- r }) }()

	for i := 0; i < 2; i++ {
		select {
		case <-results:
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for result %d; events stream close should trigger resync", i+1)
		}
	}

	mu.Lock()
	calls := listCalls
	mu.Unlock()
	if calls < 2 {
		t.Errorf("expected at least 2 list calls after events stream close, got %d", calls)
	}
	_ = p.Stop()
}

func TestDockerProvider_NonPositiveResyncDoesNotPanic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1.41/containers/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.URL.Path == "/v1.41/events":
			w.Header().Set("Content-Type", "application/json")
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	for _, resync := range []time.Duration{0, -time.Second} {
		p, err := newDockerProvider(srv.URL, "v1.41",
			ParseOptions{LabelPrefix: "gateway"}, 10*time.Millisecond, resync, zap.NewNop())
		if err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { _ = p.Start(ctx, func(Result) {}); close(done) }()

		time.Sleep(50 * time.Millisecond)
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("Start did not return for resync=%v", resync)
		}
		_ = p.Stop()
	}
}

func TestGrowBackoff(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want time.Duration
	}{
		{time.Second, 2 * time.Second},
		{2 * time.Second, 4 * time.Second},
		{20 * time.Second, 30 * time.Second},
		{30 * time.Second, 30 * time.Second},
		{time.Minute, 30 * time.Second},
	}
	for _, c := range cases {
		if got := growBackoff(c.in); got != c.want {
			t.Errorf("growBackoff(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestStreamHealthy(t *testing.T) {
	now := time.Now()
	if !streamHealthy(1, now, now) {
		t.Error("stream that delivered an event should be healthy")
	}
	if !streamHealthy(0, now.Add(-healthyStreamDuration), now) {
		t.Error("stream running >= healthyStreamDuration should be healthy")
	}
	if streamHealthy(0, now.Add(-time.Second), now) {
		t.Error("short stream without events should be unhealthy")
	}
}

func TestDockerProvider_StopWaitsForCallback(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1.41/containers/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.URL.Path == "/v1.41/events":
			w.Header().Set("Content-Type", "application/json")
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := newDockerProvider(srv.URL, "v1.41",
		ParseOptions{LabelPrefix: "gateway"}, 10*time.Millisecond, time.Hour, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	onResult := func(Result) {
		once.Do(func() {
			entered <- struct{}{}
			<-release
		})
	}
	go func() { _ = p.Start(ctx, onResult) }()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("onResult was not called")
	}

	stopDone := make(chan error, 1)
	go func() { stopDone <- p.Stop() }()

	select {
	case <-stopDone:
		t.Fatal("Stop returned while onResult was still running")
	case <-time.After(200 * time.Millisecond):
		// ожидаемо: Stop блокируется, пока onResult не завершится
	}

	close(release)

	select {
	case <-stopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return after onResult completed")
	}
}

func TestDockerProvider_StopBeforeStart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1.41/containers/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.URL.Path == "/v1.41/events":
			w.Header().Set("Content-Type", "application/json")
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := newDockerProvider(srv.URL, "v1.41",
		ParseOptions{LabelPrefix: "gateway"}, 10*time.Millisecond, time.Hour, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	called := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- p.Start(ctx, func(Result) {
			select {
			case called <- struct{}{}:
			default:
			}
		})
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after Stop")
	}
	select {
	case <-called:
		t.Fatal("onResult called after Stop")
	default:
	}
}

func TestDockerProvider_BackoffGrowsOnCleanClose(t *testing.T) {
	var mu sync.Mutex
	conns := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1.41/containers/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.URL.Path == "/v1.41/events":
			mu.Lock()
			conns++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			// Поток сразу закрывается без событий — нездоровый реконнект.
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := newDockerProvider(srv.URL, "v1.41",
		ParseOptions{LabelPrefix: "gateway"}, 10*time.Millisecond, time.Hour, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	p.initialBackoff = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = p.Start(ctx, func(Result) {}) }()

	time.Sleep(500 * time.Millisecond)
	mu.Lock()
	got := conns
	mu.Unlock()
	_ = p.Stop()

	// Экспоненциальный рост (50,100,200,400мс) даёт ~4 подключения за 500мс;
	// постоянный интервал 50мс — ~10. Порог 6 разделяет эти режимы.
	if got > 6 {
		t.Errorf("events reconnects are not backing off: got %d connections in 500ms", got)
	}
}
