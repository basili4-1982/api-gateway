package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDockerClient_ListContainers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.41/containers/json" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if !strings.Contains(r.URL.RawQuery, "gateway.enable") {
			t.Errorf("label filter missing: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{
			"Id": "abc",
			"Names": ["/svc-1"],
			"Labels": {"gateway.enable": "true", "gateway.port": "9000"},
			"Ports": [{"PrivatePort": 9000, "Type": "tcp"}],
			"NetworkSettings": {"Networks": {"backend": {"IPAddress": "10.0.0.2"}}}
		}]`))
	}))
	defer srv.Close()

	c, err := newDockerClient(srv.URL, "v1.41")
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.listContainers(context.Background(), "gateway")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "abc" {
		t.Fatalf("unexpected containers: %+v", got)
	}
	if _, ok := got[0].Networks["backend"]; !ok {
		t.Errorf("network not parsed: %+v", got[0].Networks)
	}
}

func TestDockerClient_UnknownScheme(t *testing.T) {
	if _, err := newDockerClient("tcp://1.2.3.4:2375", ""); err == nil {
		t.Fatal("expected error for unsupported scheme tcp://")
	}
}

func TestDockerClient_UnixSocket(t *testing.T) {
	c, err := newDockerClient("unix:///var/run/docker.sock", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.baseURL != "http://docker" {
		t.Fatalf("unexpected baseURL for unix host: %q", c.baseURL)
	}
	if c.http.Transport == nil {
		t.Fatal("expected custom transport for unix socket")
	}
}

func TestDockerClient_NoGlobalTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	c, err := newDockerClient(srv.URL, "v1.41")
	if err != nil {
		t.Fatal(err)
	}
	if c.http.Timeout != 0 {
		t.Fatalf("client-wide timeout must be unset to keep events stream alive, got %v", c.http.Timeout)
	}
}

func TestDockerClient_ListContainers_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, _ := newDockerClient(srv.URL, "")
	if _, err := c.listContainers(context.Background(), "gateway"); err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}

func TestDockerClient_Events(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.41/events" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte(`{"Type":"container","Action":"start","id":"abc"}` + "\n"))
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = w.Write([]byte(`{"Type":"network","Action":"connect","id":"net"}` + "\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	c, _ := newDockerClient(srv.URL, "v1.41")
	ch, err := c.events(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for a := range ch {
		actions = append(actions, a)
	}
	if len(actions) != 1 || actions[0] != "start" {
		t.Fatalf("expected only container start event, got %v", actions)
	}
}
