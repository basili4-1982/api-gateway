# Reproducible Gateway Benchmark Report Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the complete September 2026 gateway benchmark, reproducible configurations and scripts, compact source results, and a prominent link from the root README.

**Architecture:** Treat `benchmarks/2026-09-gateway-comparison/` as an immutable experiment package. Curate and sanitize the existing `/home/basili4/gwbench` artifacts instead of committing binaries or raw logs, make the report trace every table to `results/summary.csv`, and keep the root README as a concise entry point.

**Tech Stack:** Markdown, CSV, Bash, Docker, `wrk`, gateway-native YAML/config formats.

## Global Constraints

- Store the package at `benchmarks/2026-09-gateway-comparison/`.
- Do not rerun, normalize, or change completed benchmark values while documenting them.
- Record best-of-five throughput aggregation and approximately ±10% observed variance.
- Keep scenario baselines distinct.
- Commit no credentials, private endpoints, absolute local paths, binaries, Docker images, raw logs, or generated container state.
- Scripts must be non-interactive, fail clearly on missing tools, and remove only resources they create.
- Future benchmark campaigns use a new dated directory rather than modifying this package.

---

### Task 1: Curate The Benchmark Source Data

**Files:**
- Create: `benchmarks/2026-09-gateway-comparison/results/summary.csv`
- Reference: `/home/basili4/gwbench/results_final.txt`
- Reference: `/home/basili4/gwbench/results_auth.txt`
- Reference: `/home/basili4/gwbench/results_auth2.txt`
- Reference: `/home/basili4/gwbench/results_discovery.txt`
- Reference: `/home/basili4/GolandProjects/sarnas/GATEWAY_COMPARISON.md`

**Interfaces:**
- Produces: canonical rows with columns `scenario,gateway,variant,concurrency,value,unit,baseline_value,delta_percent` used by the report.

- [ ] Extract proxy-only c50/c300 throughput and c300 peak memory for all five gateways.
- [ ] Extract auth throughput for built-in JWT and external auth scenarios with their own no-auth baselines.
- [ ] Extract api-gateway RBAC, rate-limit, permission-cache, direct HTTP webhook, batched HTTP webhook, NATS, and discovery-latency results.
- [ ] Represent unavailable fields as empty CSV fields, not invented zeroes.
- [ ] Cross-check every row against the completed artifacts and current published comparison.
- [ ] Validate consistent column counts and numeric fields with a small `awk` or CSV-aware check.
- [ ] Commit the source data with a focused Conventional Commit.

### Task 2: Add Sanitized Configurations And Helpers

**Files:**
- Create: `benchmarks/2026-09-gateway-comparison/configs/api-gateway/{baseline,jwt,rbac,ratelimit,permissions-cache,permissions-no-cache,webhook-direct,webhook-batch,webhook-nats,discovery}.yaml`
- Create: `benchmarks/2026-09-gateway-comparison/configs/traefik/{static,dynamic,dynamic-forward-auth,docker-provider}.yaml`
- Create: `benchmarks/2026-09-gateway-comparison/configs/nginx/{baseline,external-auth}.conf`
- Create: `benchmarks/2026-09-gateway-comparison/configs/envoy/{baseline,jwt}.yaml`
- Create: `benchmarks/2026-09-gateway-comparison/configs/kong/{baseline,jwt}.yaml`
- Create: `benchmarks/2026-09-gateway-comparison/helpers/auth/main.go`
- Create: `benchmarks/2026-09-gateway-comparison/helpers/permissions/main.go`
- Create: `benchmarks/2026-09-gateway-comparison/helpers/webhook/main.go`
- Reference: corresponding text sources under `/home/basili4/gwbench/`

**Interfaces:**
- Produces: portable text inputs consumed by Task 3 scripts.

- [ ] Copy only configurations that correspond to reported scenarios.
- [ ] Replace machine-specific paths, private network details, and secrets with local benchmark-only values or variables.
- [ ] Keep helper sources buildable with the standard Go toolchain; do not copy compiled helper binaries.
- [ ] Document non-obvious config choices inline, especially disabled logs, worker counts, JWT keys, batching, and discovery debounce.
- [ ] Run formatting/parsing checks available for each format and `gofmt` helper sources.
- [ ] Scan the package for home-directory paths, production hosts, and credential-shaped values.
- [ ] Commit sanitized configs and helper sources with a focused Conventional Commit.

### Task 3: Add Reproduction Scripts

**Files:**
- Create: `benchmarks/2026-09-gateway-comparison/scripts/common.sh`
- Create: `benchmarks/2026-09-gateway-comparison/scripts/run-throughput.sh`
- Create: `benchmarks/2026-09-gateway-comparison/scripts/run-auth.sh`
- Create: `benchmarks/2026-09-gateway-comparison/scripts/run-features.sh`
- Create: `benchmarks/2026-09-gateway-comparison/scripts/run-discovery.sh`
- Create: `benchmarks/2026-09-gateway-comparison/scripts/cleanup.sh`
- Reference: `/home/basili4/gwbench/bench.sh`
- Reference: `/home/basili4/gwbench/bench_auth2.sh`
- Reference: `/home/basili4/gwbench/bench_rbac_rl.sh`
- Reference: `/home/basili4/gwbench/bench_perms.sh`
- Reference: `/home/basili4/gwbench/bench_webhook.sh`
- Reference: `/home/basili4/gwbench/bench_discovery.sh`

**Interfaces:**
- Consumes: Task 2 configurations and helpers.
- Produces: reproducible isolated scenarios with shared setup, requirements checks, warm-up, five measured runs, memory sampling, and scoped cleanup.

- [ ] Centralize repository-relative paths, network/container prefixes, CPU sets, memory limit, durations, and dependency checks in `common.sh`.
- [ ] Make every runner accept environment overrides while defaulting to the documented September setup.
- [ ] Ensure traps remove only containers/networks carrying the benchmark prefix.
- [ ] Print raw command output and a concise best-of-five summary without rewriting committed results.
- [ ] Run `bash -n` on every script and `shellcheck` when available.
- [ ] Exercise help/preflight paths without starting the full benchmark.
- [ ] Commit scripts with a focused Conventional Commit.

### Task 4: Write The Report And README Entry Point

**Files:**
- Create: `benchmarks/2026-09-gateway-comparison/README.md`
- Modify: `README.md`
- Reference: `docs/superpowers/specs/2026-09-13-reproducible-benchmark-report-design.md`
- Reference: `benchmarks/2026-09-gateway-comparison/results/summary.csv`

**Interfaces:**
- Consumes: Tasks 1-3.
- Produces: canonical human-readable report and one-click discovery from the repository root.

- [ ] Write the executive summary, scope, exact environment, versions, methodology, caveats, and reproduction instructions.
- [ ] Present proxy-only throughput/memory, auth, api-gateway feature costs, webhook modes, and discovery latency in separate tables with explicit baselines.
- [ ] Link each scenario to its configs/scripts and state competitor strengths and limitations neutrally.
- [ ] Add the root README section after «Возможности» with the c300 baseline, 11% Nginx gap, 15% built-in JWT cost, 89-94% external auth cost, and relative report link.
- [ ] Check all relative Markdown links and trace every reported number to `summary.csv`.
- [ ] Run `git diff --check` and review the complete package for duplicated or contradictory methodology.
- [ ] Commit report and README with a focused Conventional Commit.

### Task 5: Review And Publish The Documentation

**Files:**
- Review: `benchmarks/2026-09-gateway-comparison/**`
- Review: `README.md`

**Interfaces:**
- Consumes: complete validated benchmark package.
- Produces: merged documentation PR in `sarnas-it/api-gateway`.

- [ ] Invoke `requesting-code-review` and resolve findings without altering measured evidence.
- [ ] Run Go tests, `go vet`, script syntax checks, secret/path scans, CSV validation, and relative-link validation.
- [ ] Inspect `git status`, branch diff, and recent log; ensure no scratch binaries or raw logs are tracked.
- [ ] Push the branch to the user fork and `sarnas-it/api-gateway`, then open a PR against `master` with methodology and verification details.
- [ ] Merge after CI passes and verify the root README link and benchmark report on GitHub.
