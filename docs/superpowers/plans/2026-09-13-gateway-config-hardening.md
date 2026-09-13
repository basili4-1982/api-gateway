# Gateway Config Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make api-gateway configuration truthful and complete: remove the dead `forward_headers` field, honor `max_request_body_size: 0` as unlimited, implement real ACME staging, validate `logging.format`, warn on unknown YAML keys, and implement weighted balancing across static targets sharing a route.

**Architecture:** Fixes are ordered so the configuration reference written afterwards describes real behavior. Weighted balancing is the largest change: group routing rules that share `(host, path_prefix, methods)` into a route with a candidate target pool, and select a healthy target with weighted round-robin at request time.

**Tech Stack:** Go 1.25, `gopkg.in/yaml.v3`, `golang.org/x/crypto/acme/autocert`, `go.uber.org/zap`, `httputil.ReverseProxy`.

## Global Constraints

- Do not change defaults for existing valid configs except where this plan explicitly changes documented semantics.
- Preserve `FindTargetForPath` signature; `serveStatic` (`multi_proxy.go:965`) depends on it.
- Keep `routeByRule` pointer-identity semantics between the loaded config and build-time route configs.
- Any per-route selection state must be concurrency-safe (`atomic`), because requests run in parallel.
- Extend `targetChanged` for any new field that affects selection.
- Every change ships with tests; run `go build ./...`, `go vet ./...`, `go test -race ./...`, and `golangci-lint run`.
- No new lint findings.

---

### Task 1: Remove `headers.forward_headers`

**Files:**
- Modify: `internal/config/config.go:181`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `HeadersConfig` without `ForwardHeaders`; behavior otherwise unchanged.

- [ ] Confirm no reader exists: `grep -rn "ForwardHeaders\|forward_headers" internal/ cmd/`.
- [ ] Delete the `ForwardHeaders` field.
- [ ] Add a test asserting a config that sets `headers.forward_headers` still loads (it will be an unknown key handled in Task 6) and that no behavior references it.
- [ ] Run `go build ./... && go test -race ./internal/config/`.
- [ ] Commit `refactor(config): remove unused headers.forward_headers`.

### Task 2: `max_request_body_size: 0` means unlimited

**Files:**
- Modify: `internal/config/config.go:87-93,297-299`
- Modify: `internal/proxy/middleware_chain.go:301-308`
- Test: `internal/config/config_test.go`, `internal/proxy/proxy_test.go`

**Interfaces:**
- Produces: `ServerConfig.MaxRequestBodySize *int64`; `nil` = default 10 MiB, `0` = unlimited, `>0` = byte limit. A helper `func (s ServerConfig) EffectiveMaxRequestBodySize() int64` returns the applied limit (`<=0` means unlimited).

- [ ] Write a failing test: omitted → 10 MiB; explicit `0` → unlimited; explicit `N` → `N`.
- [ ] Change the field to `*int64`; set default 10 MiB only when `nil`.
- [ ] Update enforcement in `proxyRequest` to use the effective value and keep `<=0` as unlimited.
- [ ] Bound the audit `io.ReadAll` in `proxyHandler` with the same limit (or `http.MaxBytesReader`) so audit does not read unbounded bodies.
- [ ] Run config and proxy tests with `-race`.
- [ ] Commit `fix(config): honor max_request_body_size 0 as unlimited`.

### Task 3: Validate `logging.format`

**Files:**
- Modify: `internal/config/config.go` (`validate`), `internal/logger/zap_logger.go:52-58`
- Modify: `config.local.example.yaml` (comment)
- Test: `internal/config/config_test.go`, add `internal/logger/zap_logger_test.go`

**Interfaces:**
- Produces: accepted values `console`, `text` (alias), `json`; `validate()` returns an error naming the field and value otherwise.

- [ ] Write a failing test: unknown format fails validation; `console`, `text`, `json` pass.
- [ ] Add the check in `validate()`.
- [ ] Make `NewZapLogger` treat `text` and `console` identically and document valid values.
- [ ] Run tests with `-race`.
- [ ] Commit `fix(config): validate logging.format`.

### Task 4: Real ACME staging

**Files:**
- Modify: `internal/proxy/tls.go:14-19,46`
- Modify: `internal/config/config.go:49-58` (optional `directory_url`)
- Test: `internal/proxy/tls_test.go` (new)

**Interfaces:**
- Produces: when `tls.staging: true`, the autocert client uses `https://acme-staging-v02.api.letsencrypt.org/directory`; an optional `tls.directory_url` overrides it. Staging certs cache under a `staging/` subdirectory of `cache_dir`.

- [ ] Write a failing test asserting the staging directory URL and cache subdir selection.
- [ ] Extract a helper `func buildCertManager(tls *config.TLSConfig) *autocert.Manager` so it is testable without starting a server.
- [ ] Set `certManager.Client = &acme.Client{DirectoryURL: ...}` when staging, and use the staging cache subdir.
- [ ] Document `directory_url` if added.
- [ ] Run tests with `-race`.
- [ ] Commit `feat(tls): implement ACME staging directory`.

### Task 5: Warn on unknown YAML keys

**Files:**
- Modify: `internal/config/config.go:249-278`
- Modify: `cmd/main.go:34,106`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `func Load(path string) (*Config, []string, error)` where the second value is unknown-key warnings with dotted paths (e.g. `headers.forward_headers`). Callers log them at warn level. Keep a thin `LoadStrict`/internal helper as needed.

- [ ] Write a failing test: a config with an unknown top-level and nested key yields warnings naming both paths; a valid config yields none.
- [ ] Implement known-key collection by walking the YAML node tree against struct tags (reflection), producing all unknown keys with paths.
- [ ] Update `Load` signature and both `cmd/main.go` call sites to log warnings.
- [ ] Verify the embedded `application` struct and all nested structs are fully tagged.
- [ ] Run `go build ./...`, `go test -race ./...`.
- [ ] Commit `feat(config): warn on unknown YAML keys`.

### Task 6: Weighted balancing across static targets sharing a route

**Files:**
- Modify: `internal/config/merge.go:34-58` (dedup key)
- Modify: `internal/proxy/multi_proxy.go:29-33,148-156,838-860`
- Modify: `internal/proxy/middleware_chain.go:237-266`
- Modify: `internal/config/config.go` (doc comment for `Weight`)
- Modify: `README.md` (balancing description)
- Test: `internal/proxy/proxy_test.go`, `internal/config/merge_test.go`, `internal/config/config_test.go`

**Interfaces:**
- Consumes: `TargetConfig.Weight int` (existing, default `1`).
- Produces: `RouteConfig` gains `Targets []*TargetProxy`, `weights []int`, `totalWeight int`, `counter atomic.Uint64`. A method `func (rc *RouteConfig) pickTarget() *TargetProxy` returns the next healthy target by weighted round-robin, or `nil` if none are healthy.
- Produces: multiple routing rules with the same `(host, path_prefix, methods)` form one route pool; `Merge` dedups on `(host, path_prefix, methods, target_name)`.

- [ ] Write failing tests: two rules sharing a route with weights 3 and 1 route roughly 3:1 over many selections; unhealthy targets are skipped; all-unhealthy returns the existing 503 path; `weight: 0` is never selected.
- [ ] Change `Merge` dedup key to include the target name so pooled rules survive.
- [ ] Group rules into pools when building `RouteConfig`; populate `Targets`, weights, and total weight.
- [ ] Implement `pickTarget` with weighted round-robin that skips `!isHealthy(cbEnabled)` and falls back to any candidate only if none are healthy.
- [ ] Update `proxyHandler` to select via the route's pool and keep the chosen target config in sync for `modifyRequest` and `logAccess`.
- [ ] Extend `targetChanged` and `rebuildRouteConfigs` so weight/group changes take effect on reload.
- [ ] Run `go build ./...`, `go vet ./...`, `go test -race ./...`, `golangci-lint run`.
- [ ] Commit `feat(proxy): weighted balancing for static target pools`.

### Task 7: Final review and PR

**Files:**
- Review all changes.

- [ ] Run the full check suite and `git diff --check`.
- [ ] Invoke `requesting-code-review`; resolve findings.
- [ ] Commit any remaining fixes with Conventional Commit messages.
- [ ] Push the branch to the user fork and `sarnas-it/api-gateway`; open a PR against `master` describing behavior changes and tests; merge after CI passes.
