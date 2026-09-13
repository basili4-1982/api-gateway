package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/basili4-1982/api-gateway/internal/dashboard"
)

func TestRunGracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	srv := dashboard.NewServer(dashboard.ServerConfig{})
	var out bytes.Buffer
	if err := run(ctx, "127.0.0.1:0", srv, &out); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !strings.Contains(out.String(), "listening") {
		t.Errorf("output = %q, want a listening message", out.String())
	}
}

func TestRunListenError(t *testing.T) {
	srv := dashboard.NewServer(dashboard.ServerConfig{})
	var out bytes.Buffer
	if err := run(context.Background(), "invalid-address", srv, &out); err == nil {
		t.Fatal("run() error = nil, want listen error")
	}
}
