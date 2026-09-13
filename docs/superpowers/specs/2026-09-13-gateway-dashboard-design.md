# Read-Only Gateway Dashboard Design

## Goal

Provide a Traefik-style overview dashboard for api-gateway as a **separate binary**, so the gateway process stays lean. The dashboard shows routes, targets, discovery state, configuration summary, and live metrics. It is read-only and needs no changes to the gateway.

## Scope

- Read-only overview. No config editing, no actions, no route mutation.
- Separate Go binary `cmd/dashboard` in the api-gateway repository, built and shipped independently of `cmd/`.
- Server-rendered HTML (Go `html/template`, embedded) plus a JSON status endpoint. No Node build step.

## Data Sources

The dashboard is a client of existing artifacts and endpoints:

1. **Config file** — parsed with `internal/config.Load` for the summary: server port, TLS enabled, discovery enabled, permissions enabled, number of static targets and routing rules, webhook count and transports.
2. **Discovery state file** — the last-known-good JSON written by the gateway (`discovery.state_file`). Shows discovered targets and rules. The JSON uses Go field names, so the dashboard decodes into `config.TargetConfig` / `config.RoutingRule` (or a dedicated struct with the same field names).
3. **Gateway `/metrics`** — scraped over HTTP. Parses the expvar text output: `gateway_requests_total` (map), `gateway_request_duration_ms` (map), `gateway_rate_limit_denials_total`, `gateway_active_requests`.

Target health (`gateway_target_up`) is currently omitted by the gateway metrics handler, so the dashboard shows health as unknown. Exposing it is a possible follow-up gateway change, out of scope here.

## Command-Line Interface

- `-config` (default `/etc/proxy/config.yaml`) — gateway config to summarize.
- `-discovery-state` (default empty) — path to the discovery state file; empty disables that panel.
- `-metrics-url` (default `http://127.0.0.1:8080/metrics`) — gateway metrics endpoint; empty disables the metrics panel.
- `-listen` (default `127.0.0.1:8081`) — dashboard bind address.
- `-refresh` (default `5s`) — UI auto-refresh interval.
- `-basic-auth` (default empty) — optional `user:password`; when set, all routes require HTTP Basic Auth.

The default bind is loopback for safety. Container deployments set `-listen :8081` and restrict network access or enable `-basic-auth`.

## Endpoints

- `GET /` — HTML overview: header with gateway reachability, config summary, discovered targets and rules, metrics cards and per-path/method tables.
- `GET /api/status` — the same data as JSON, for scripting and the UI refresh.
- `GET /healthz` — `200 ok` when the dashboard process is up.

## Behavior

- Each request (or refresh) re-reads the config, the state file, and `/metrics`, so the view is current. Failures of one source degrade gracefully: the panel shows an error, the rest still renders.
- If the config cannot be loaded, the page still renders with an explicit error banner (the dashboard does not require a valid gateway config to start).
- No secrets are rendered: the config summary shows field presence and non-secret values only; `jwt.secret_key`, `basic_auth.password`, `permissions.api_key`, and similar values are never printed.

## Non-Goals

- Editing or reloading configuration.
- Actions such as permission-cache invalidation (the gateway already exposes that endpoint; the dashboard does not call it).
- Authentication beyond optional Basic Auth.
- Persisting history; metrics are shown as current snapshots.

## Success Criteria

- `go build ./cmd/dashboard` produces a standalone binary that runs without the gateway running.
- The overview renders config summary, discovery state, and metrics, degrading gracefully when a source is missing.
- `/api/status` returns valid JSON matching the rendered data.
- No secret values appear in HTML or JSON.
- Tests cover metrics parsing, discovery-state decoding, handler responses, Basic Auth, and graceful degradation.
