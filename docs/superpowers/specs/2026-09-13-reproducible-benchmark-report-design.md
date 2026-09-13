# Reproducible Gateway Benchmark Report Design

## Goal

Publish the complete September 2026 benchmark in the `sarnas-it/api-gateway` repository and link it prominently from the root README. The report must preserve the measured evidence and include enough configuration and automation for another engineer to reproduce the experiment.

## Location

Create a versioned benchmark package at:

`benchmarks/2026-09-gateway-comparison/`

The package is immutable historical evidence for this test environment. Future benchmark campaigns should use a new dated directory instead of overwriting these results.

The root `README.md` will contain a short section named «Производительность и воспроизводимый бенчмарк» after the feature list. It will state the baseline and built-in JWT headline results and link to the package report.

## Package Contents

### Report

`benchmarks/2026-09-gateway-comparison/README.md` is the entry point and canonical report. It contains:

1. Executive summary and scope.
2. Exact host, container, CPU, memory, backend, client, image-version, warm-up, duration, and aggregation methodology.
3. Proxy-only throughput at c50 and c300 and peak memory at c300.
4. Built-in JWT versus external synchronous auth results.
5. Incremental api-gateway costs for RBAC, route rate limiting, permission cache, HTTP webhooks without batching, HTTP batching with backpressure, and NATS.
6. Docker service-discovery reaction latency.
7. Interpretation, limitations, expected run-to-run variance, and guidance for reproducing the benchmark.

The report distinguishes measured facts from architectural interpretation and does not hide competitors' advantages.

### Configurations

`benchmarks/2026-09-gateway-comparison/configs/` contains the exact text configurations needed for the reported scenarios:

- api-gateway baseline and feature scenarios;
- Traefik, Nginx, Envoy, and Kong baseline scenarios;
- native JWT scenarios where supported;
- external auth scenarios for Nginx and Traefik;
- webhook batching and discovery scenarios;
- Docker Compose or equivalent service definitions for supporting backend, auth, webhook, and discovery containers.

No credentials, private endpoints, generated state, or environment-specific absolute paths may be committed.

### Scripts

`benchmarks/2026-09-gateway-comparison/scripts/` contains small non-interactive scripts for:

- preparing and tearing down the isolated benchmark environment;
- warming up and running c50/c300 `wrk` samples;
- collecting peak container memory;
- running feature-cost and discovery-latency scenarios;
- aggregating the reported summary.

Scripts must fail clearly when required tools are missing and clean up only resources they create.

### Results

`benchmarks/2026-09-gateway-comparison/results/summary.csv` contains the compact source values used by the report tables. It includes scenario, gateway, concurrency, throughput or latency value, unit, and comparison baseline where applicable.

Do not commit raw `wrk` logs, binaries, Docker images, temporary container state, or other heavy runtime artifacts.

## Data Integrity

- Copy values from the completed benchmark artifacts and the currently published comparison; do not rerun or silently normalize values while documenting them.
- Record that throughput values are the best of five runs and that observed variance is approximately ±10%.
- Keep proxy-only, authentication, feature-cost, webhook, and discovery scenarios distinct because they use different baselines.
- State that the benchmark measures gateway overhead against a near-zero-latency backend and does not predict application throughput.
- Include exact software versions and relevant non-default settings such as Envoy concurrency, Kong DB-less mode, disabled access logs, CPU pinning, and memory limits.

## README Presentation

The root README section should remain concise:

- one sentence explaining the benchmark scope;
- baseline c300 result for api-gateway and the 11% gap to Nginx;
- built-in JWT cost of 15% compared with 89-94% for the measured external auth paths;
- a relative link to `benchmarks/2026-09-gateway-comparison/README.md` labelled as the full reproducible report.

The README must not duplicate all tables, because the dated package is the canonical technical record.

## Success Criteria

- A new contributor can find the report from the root README in one click.
- The report covers every completed scenario and explains which baseline each percentage uses.
- Configurations and scripts contain no private infrastructure details and are sufficient to reconstruct the benchmark topology.
- Every table value can be traced to `results/summary.csv`.
- Repository checks pass and all shell scripts pass syntax validation.
