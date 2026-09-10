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
