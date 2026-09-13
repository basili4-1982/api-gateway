# Gateway Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a separate read-only dashboard binary that summarizes gateway config, discovery state, and metrics.

**Architecture:** A new `cmd/dashboard` entry point and an `internal/dashboard` package. The package defines a `Status` model, three data collectors (config, discovery state, metrics), an HTML template, and an HTTP server. The gateway binary is untouched.

**Tech Stack:** Go 1.25, `html/template` with `embed`, `net/http`, `encoding/json`, `internal/config`.

## Global Constraints

- Use `-buildvcs=false` for Go commands in worktrees.
- Do not modify the gateway binary or its behavior.
- Never render secret values (`jwt.secret_key`, `basic_auth.password`, `permissions.api_key`, `permissions.invalidate_token`).
- Default bind is `127.0.0.1:8081`.
- Each data source degrades gracefully; the dashboard starts even without a valid config.
- Run `go build ./...`, `go vet ./...`, `go test -race ./...`, `golangci-lint run`.

---

### Task 1: Status model and collectors

**Files:**
- Create: `internal/dashboard/status.go`
- Create: `internal/dashboard/config_source.go`
- Create: `internal/dashboard/state_source.go`
- Create: `internal/dashboard/metrics_source.go`
- Test: `internal/dashboard/*_test.go`

**Interfaces:**
- Produces: `type Status struct { Config *ConfigSummary; Discovery *DiscoverySummary; Metrics *MetricsSummary; Errors []string }` and collector functions `LoadConfigSummary(path string) (*ConfigSummary, error)`, `LoadDiscoverySummary(path string) (*DiscoverySummary, error)`, `ScrapeMetrics(url string) (*MetricsSummary, error)`.

- [ ] Write failing tests for metrics parsing (expvar text: `gateway_requests_total {…}`, `gateway_active_requests N`, `gateway_rate_limit_denials_total N`), discovery-state JSON decoding, and config summary (counts, flags, no secrets).
- [ ] Implement `ConfigSummary` (port, tls, discovery, permissions, targets/rules counts, webhooks) via `config.Load`; never copy secret fields.
- [ ] Implement `DiscoverySummary` decoding the state file into `config.TargetConfig`/`config.RoutingRule` slices.
- [ ] Implement `ScrapeMetrics` with a short timeout, parsing expvar text.
- [ ] Run tests with `-race`.
- [ ] Commit `feat(dashboard): status model and data collectors`.

### Task 2: HTTP server, template, and CLI

**Files:**
- Create: `internal/dashboard/server.go`
- Create: `internal/dashboard/templates/overview.html`
- Create: `cmd/dashboard/main.go`
- Test: `internal/dashboard/server_test.go`, `cmd/dashboard/main_test.go`

**Interfaces:**
- Consumes: Task 1 collectors.
- Produces: `func NewServer(cfg ServerConfig) *Server` with `Handler() http.Handler`; `GET /`, `GET /api/status`, `GET /healthz`; optional Basic Auth.

- [ ] Write failing handler tests: `/healthz` 200; `/api/status` valid JSON with errors array; `/` renders HTML with summary; Basic Auth 401/200; graceful degradation when sources fail.
- [ ] Implement `Server` assembling `Status` per request with a bounded timeout.
- [ ] Implement the embedded HTML template (no external assets) with a simple, clean layout and auto-refresh.
- [ ] Implement `cmd/dashboard/main.go` flags: `-config`, `-discovery-state`, `-metrics-url`, `-listen`, `-refresh`, `-basic-auth`; graceful shutdown on SIGINT/SIGTERM.
- [ ] Run tests with `-race`; build `./cmd/dashboard`.
- [ ] Commit `feat(dashboard): overview server and CLI`.

### Task 3: Packaging and docs

**Files:**
- Create: `Dockerfile.dashboard` (or a documented build target)
- Modify: `Makefile` (add `dashboard` and `dashboard-run` targets)
- Modify: `README.md` (a short «Дашборд» section)

- [ ] Add a minimal image/target that builds and runs `cmd/dashboard`.
- [ ] Add Makefile targets.
- [ ] Document flags, data sources, security defaults, and a docker-compose example with shared config/state volumes and `-metrics-url`.
- [ ] Run `go build ./...`, `go vet ./...`, `go test -race ./...`, `golangci-lint run`, `git diff --check`.
- [ ] Commit `docs(dashboard): usage and packaging`.

### Task 4: Review and PR

- [ ] Invoke `requesting-code-review`; resolve findings.
- [ ] Push and open a PR against `master`; merge after checks pass.
