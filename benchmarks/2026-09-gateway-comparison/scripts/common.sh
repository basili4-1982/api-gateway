#!/usr/bin/env bash
# Shared setup for the September 2026 gateway comparison benchmark.
#
# Source this file from a runner; do not execute it directly. It centralizes:
#   - repository-relative paths (nothing depends on where the repo is checked out);
#   - the benchmark resource prefix used for containers, the network and images;
#   - CPU pinning and the memory limit for the gateway/client/backend roles;
#   - the documented c50/c300 wrk profile, warm-up, repetitions and memory sampling;
#   - dependency checks, container helpers and prefix-scoped cleanup.
#
# Every setting can be overridden through the environment, but the defaults are
# the exact September 2026 setup recorded in results/summary.csv:
#   gateway 0-1 (768m), client 2-7, backend 8-11
#   warm-up 5s; c50 = wrk -t2 -c50 -d15s; c300 = wrk -t4 -c300 -d20s
#   best of 5 runs; peak memory sampled with `docker stats`
#
# Runners never write to results/summary.csv. Set RESULTS to append a plain-text
# run log somewhere writable (for example /tmp), otherwise summaries go to stdout.

# Many variables and result globals defined here are consumed by the runners that
# source this file; shellcheck cannot see those uses when checking common.sh alone.
# shellcheck disable=SC2034
set -uo pipefail

# --- repository-relative paths -------------------------------------------------

COMMON_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PKG_DIR="$(cd "${COMMON_DIR}/.." && pwd)"
REPO_ROOT="$(cd "${PKG_DIR}/../.." && pwd)"
CONFIG_DIR="${PKG_DIR}/configs"
HELPER_DIR="${PKG_DIR}/helpers"

# --- benchmark namespace -------------------------------------------------------

# Containers, the network and locally built images carry this prefix, which is
# what cleanup matches on. Change PREFIX to run several campaigns side by side.
PREFIX="${PREFIX:-gwbench}"
NET="${NET:-${PREFIX}-net}"

# Scratch directory for helper binaries and generated wrk Lua scripts. It is a
# private temp path (never committed) removed by cleanup_benchmark. The path is
# stable for a given PREFIX so `cleanup.sh` can remove leftovers too.
HELPER_BUILD_DIR="${HELPER_BUILD_DIR:-${TMPDIR:-/tmp}/gwbench-${PREFIX}-helpers}"

GW_CONTAINER="${PREFIX}-gw"
BACKEND_CONTAINER="${PREFIX}-backend"
AUTH_CONTAINER="${PREFIX}-auth"
PERMS_CONTAINER="${PREFIX}-perms"
SINK_CONTAINER="${PREFIX}-sink"
NATS_CONTAINER="${PREFIX}-nats"
POD_CONTAINER="${PREFIX}-pod"

# --- resource limits -----------------------------------------------------------

GATEWAY_CPUS="${GATEWAY_CPUS:-0-1}"
CLIENT_CPUS="${CLIENT_CPUS:-2-7}"
BACKEND_CPUS="${BACKEND_CPUS:-8-11}"
GATEWAY_MEMORY="${GATEWAY_MEMORY:-768m}"

# --- documented load profile ---------------------------------------------------

WARMUP_DURATION="${WARMUP_DURATION:-5s}"
C50_THREADS="${C50_THREADS:-2}"
C50_CONNECTIONS="${C50_CONNECTIONS:-50}"
C50_DURATION="${C50_DURATION:-15s}"
C300_THREADS="${C300_THREADS:-4}"
C300_CONNECTIONS="${C300_CONNECTIONS:-300}"
C300_DURATION="${C300_DURATION:-20s}"
REPS="${REPS:-5}"
MEM_SAMPLE_INTERVAL="${MEM_SAMPLE_INTERVAL:-0.4}"

# --- images --------------------------------------------------------------------

GATEWAY_IMAGE="${GATEWAY_IMAGE:-${PREFIX}/api-gateway:bench}"
BACKEND_IMAGE="${BACKEND_IMAGE:-traefik/whoami:latest}"
AUTH_IMAGE="${AUTH_IMAGE:-traefik/whoami:latest}"
HELPER_BASE_IMAGE="${HELPER_BASE_IMAGE:-alpine:latest}"
TRAEFIK_IMAGE="${TRAEFIK_IMAGE:-traefik:v3.1}"
NGINX_IMAGE="${NGINX_IMAGE:-nginx:1.27-alpine}"
ENVOY_IMAGE="${ENVOY_IMAGE:-envoyproxy/envoy:v1.31-latest}"
KONG_IMAGE="${KONG_IMAGE:-kong:3.7}"
NATS_IMAGE="${NATS_IMAGE:-nats:2.10-alpine}"

# --- logging -------------------------------------------------------------------

log() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

# --- dependency checks ---------------------------------------------------------

require_cmd() {
  local missing=0 cmd
  for cmd in "$@"; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
      printf 'ERROR: required command not found: %s\n' "$cmd" >&2
      missing=1
    fi
  done
  [ "$missing" -eq 0 ] || die "install the missing dependencies and retry"
}

# Base preflight. Runners that also build helpers call require_cmd go explicitly.
preflight() {
  require_cmd docker wrk taskset curl
  docker info >/dev/null 2>&1 || die "docker daemon is not reachable"
  log "preflight OK: docker, wrk, taskset, curl"
}

# --- images --------------------------------------------------------------------

# Use the existing api-gateway image, otherwise build it from the repository root.
ensure_gateway_image() {
  if docker image inspect "$GATEWAY_IMAGE" >/dev/null 2>&1; then
    log "using existing gateway image $GATEWAY_IMAGE"
    return 0
  fi
  log "building gateway image $GATEWAY_IMAGE from $REPO_ROOT"
  docker build -t "$GATEWAY_IMAGE" "$REPO_ROOT" >&2 || die "gateway image build failed"
}

# Build a helper command as a static linux/amd64 binary into a private temp dir.
# Binaries are never committed and the directory is removed by cleanup_benchmark.
ensure_helper_binary() { # name source-dir -> prints binary path
  local name=$1 src=$2 bin rel
  mkdir -p "$HELPER_BUILD_DIR"
  bin="${HELPER_BUILD_DIR}/${name}"
  case "$src" in
    "${REPO_ROOT}"/*) rel="./${src#"${REPO_ROOT}"/}" ;;
    *) rel="$src" ;;
  esac
  if [ ! -x "$bin" ]; then
    log "building helper ${name} from ${rel}"
    ( cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
      go build -buildvcs=false -o "$bin" "$rel" ) \
      || die "helper build failed: ${name}"
  fi
  printf '%s' "$bin"
}

# --- benchmark client scripts --------------------------------------------------

# The tokens below are benchmark-only HS256 tokens signed with the non-secret key
# "benchsecret" that the sanitized configs use. They are not credentials. They are
# written to a temp dir so the scripts stay self-contained.
ensure_lua() { # jwt|rbac|perms -> prints path to the lua script
  local name=$1 file token
  mkdir -p "$HELPER_BUILD_DIR"
  file="${HELPER_BUILD_DIR}/${name}.lua"
  if [ ! -f "$file" ]; then
    case "$name" in
      jwt)   token="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIiwiaXNzIjoiYmVuY2giLCJleHAiOjIwMDAwMDAwMDB9.djvuU_BdEzwNcjNsk7RD1AM3YmaqZsivUbaf3HhodWg" ;;
      rbac)  token="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIiwiaXNzIjoiYmVuY2giLCJleHAiOjIwMDAwMDAwMDAsInJvbGVzIjpbImFkbWluIl19.HYdrvQTpmGNJWKkLPD7yAOD0nHliEmr8123qaWO5iFA" ;;
      perms) token="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpZCI6MSwiaXNzIjoiYmVuY2giLCJleHAiOjIwMDAwMDAwMDB9.sN1p6XYQCCR3fKa0rdJlYcRmugtsVjmXKptK277TzUA" ;;
      *) die "unknown lua script: ${name}" ;;
    esac
    cat > "$file" <<EOF
wrk.method = "GET"
wrk.headers["Authorization"] = "Bearer ${token}"
EOF
  fi
  printf '%s' "$file"
}

# --- docker helpers ------------------------------------------------------------

start_network() {
  if ! docker network inspect "$NET" >/dev/null 2>&1; then
    docker network create "$NET" >/dev/null || die "cannot create network $NET"
    log "created network $NET"
  fi
}

docker_ip() {
  docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$1"
}

wait_http() { # url [attempts]
  local url=$1 attempts=${2:-120} i
  for ((i = 0; i < attempts; i++)); do
    if curl -s -o /dev/null --max-time 1 "$url" 2>/dev/null; then
      return 0
    fi
    sleep 0.5
  done
  return 1
}

gateway_url() { # uses GW_CONTAINER and GW_PORT
  printf 'http://%s:%s/' "$(docker_ip "$GW_CONTAINER")" "$GW_PORT"
}

# --- prefix-scoped cleanup -----------------------------------------------------

cleanup_benchmark() {
  local ids=()
  mapfile -t ids < <(docker ps -aq --filter "name=${PREFIX}-" 2>/dev/null || true)
  if [ "${#ids[@]}" -gt 0 ]; then
    log "removing benchmark containers: ${#ids[@]}"
    docker rm -f "${ids[@]}" >/dev/null 2>&1 || true
  fi
  if docker network inspect "$NET" >/dev/null 2>&1; then
    log "removing benchmark network $NET"
    docker network rm "$NET" >/dev/null 2>&1 || true
  fi
  if [ -n "${HELPER_BUILD_DIR:-}" ] && [ -d "$HELPER_BUILD_DIR" ]; then
    rm -rf "$HELPER_BUILD_DIR"
  fi
}

trap_benchmark_cleanup() {
  trap 'cleanup_benchmark' EXIT
  trap 'cleanup_benchmark; exit 130' INT TERM
}

# --- supporting containers -----------------------------------------------------

start_backend() {
  docker rm -f "$BACKEND_CONTAINER" >/dev/null 2>&1 || true
  docker run -d --name "$BACKEND_CONTAINER" --network "$NET" --network-alias backend \
    --cpuset-cpus="$BACKEND_CPUS" "$BACKEND_IMAGE" >/dev/null || die "cannot start backend"
}

# External auth responders (Nginx auth_request, Traefik forwardAuth) need a service
# listening on :80 because the committed configs point at `auth:80`. traefik/whoami
# is the exact responder used for the reported numbers.
start_auth() {
  docker rm -f "$AUTH_CONTAINER" >/dev/null 2>&1 || true
  docker run -d --name "$AUTH_CONTAINER" --network "$NET" --network-alias auth \
    --cpuset-cpus="$BACKEND_CPUS" "$AUTH_IMAGE" >/dev/null || die "cannot start auth"
}

# Permission service from helpers/auth, listening on :9000 (configs use perms:9000).
start_perms() {
  local bin
  bin="$(ensure_helper_binary auth "${HELPER_DIR}/auth")"
  docker rm -f "$PERMS_CONTAINER" >/dev/null 2>&1 || true
  docker run -d --name "$PERMS_CONTAINER" --network "$NET" --network-alias perms \
    --cpuset-cpus="$BACKEND_CPUS" -v "${bin}:/perms:ro" "$HELPER_BASE_IMAGE" /perms \
    >/dev/null || die "cannot start permission service"
}

# Webhook sink from helpers/webhook, listening on :9000 (configs use sink:9000).
start_sink() {
  local bin
  bin="$(ensure_helper_binary webhook "${HELPER_DIR}/webhook")"
  docker rm -f "$SINK_CONTAINER" >/dev/null 2>&1 || true
  docker run -d --name "$SINK_CONTAINER" --network "$NET" --network-alias sink \
    --cpuset-cpus="$BACKEND_CPUS" -v "${bin}:/sink:ro" "$HELPER_BASE_IMAGE" /sink \
    >/dev/null || die "cannot start webhook sink"
}

start_nats() {
  docker rm -f "$NATS_CONTAINER" >/dev/null 2>&1 || true
  docker run -d --name "$NATS_CONTAINER" --network "$NET" --network-alias nats \
    --cpuset-cpus="$BACKEND_CPUS" "$NATS_IMAGE" >/dev/null || die "cannot start nats"
}

# --- gateway containers --------------------------------------------------------

start_gateway_api() { # config-path
  docker rm -f "$GW_CONTAINER" >/dev/null 2>&1 || true
  docker run -d --name "$GW_CONTAINER" --network "$NET" \
    --cpuset-cpus="$GATEWAY_CPUS" --memory="$GATEWAY_MEMORY" \
    -v "$1:/etc/proxy/config.yaml:ro" "$GATEWAY_IMAGE" >/dev/null \
    || die "cannot start api-gateway"
  GW_PORT=8080
}

start_gateway_traefik() { # static-config dynamic-config
  docker rm -f "$GW_CONTAINER" >/dev/null 2>&1 || true
  docker run -d --name "$GW_CONTAINER" --network "$NET" \
    --cpuset-cpus="$GATEWAY_CPUS" --memory="$GATEWAY_MEMORY" \
    -v "$1:/etc/traefik/traefik.yml:ro" \
    -v "$2:/etc/traefik/dynamic.yml:ro" "$TRAEFIK_IMAGE" >/dev/null \
    || die "cannot start traefik"
  GW_PORT=80
}

start_gateway_nginx() { # config-path
  docker rm -f "$GW_CONTAINER" >/dev/null 2>&1 || true
  docker run -d --name "$GW_CONTAINER" --network "$NET" \
    --cpuset-cpus="$GATEWAY_CPUS" --memory="$GATEWAY_MEMORY" \
    -v "$1:/etc/nginx/nginx.conf:ro" "$NGINX_IMAGE" >/dev/null \
    || die "cannot start nginx"
  GW_PORT=80
}

start_gateway_envoy() { # config-path
  docker rm -f "$GW_CONTAINER" >/dev/null 2>&1 || true
  docker run -d --name "$GW_CONTAINER" --network "$NET" \
    --cpuset-cpus="$GATEWAY_CPUS" --memory="$GATEWAY_MEMORY" \
    -v "$1:/etc/envoy/envoy.yaml:ro" "$ENVOY_IMAGE" \
    -c /etc/envoy/envoy.yaml --concurrency 2 --log-level error >/dev/null \
    || die "cannot start envoy"
  GW_PORT=80
}

start_gateway_kong() { # config-path
  docker rm -f "$GW_CONTAINER" >/dev/null 2>&1 || true
  docker run -d --name "$GW_CONTAINER" --network "$NET" \
    --cpuset-cpus="$GATEWAY_CPUS" --memory="$GATEWAY_MEMORY" \
    -e KONG_DATABASE=off \
    -e KONG_DECLARATIVE_CONFIG=/kong.yml \
    -e KONG_PROXY_LISTEN='0.0.0.0:8000 reuseport backlog=16384' \
    -e KONG_ADMIN_LISTEN=off \
    -e KONG_PROXY_ACCESS_LOG=/dev/null \
    -e KONG_ADMIN_ACCESS_LOG=/dev/null \
    -e KONG_PROXY_ERROR_LOG=/dev/stderr \
    -e KONG_ADMIN_ERROR_LOG=/dev/stderr \
    -e KONG_NGINX_WORKER_PROCESSES=2 \
    -v "$1:/kong.yml:ro" "$KONG_IMAGE" >/dev/null \
    || die "cannot start kong"
  GW_PORT=8000
}

gw_down() {
  docker rm -f "$GW_CONTAINER" >/dev/null 2>&1 || true
}

wait_gateway() { # label
  local label=$1 url
  url="$(gateway_url)"
  log "${label}: waiting for ${url}"
  if ! wait_http "$url"; then
    docker logs "$GW_CONTAINER" 2>&1 | tail -20 >&2
    die "${label} did not become ready"
  fi
}

# --- measurement ---------------------------------------------------------------

warmup() { # url [lua-script]
  local url=$1 script=${2:-}
  local -a args=(wrk -t"$C50_THREADS" -c"$C50_CONNECTIONS" -d"$WARMUP_DURATION")
  [ -n "$script" ] && args+=(-s "$script")
  args+=("$url")
  log "warm-up ${WARMUP_DURATION}: ${args[*]}"
  taskset -c "$CLIENT_CPUS" "${args[@]}" >/dev/null 2>&1 || true
}

# One wrk sample. Raw output goes to stdout and $out; sets WRK_LAST_RPS.
wrk_sample() { # url script threads connections duration outfile
  local url=$1 script=$2 threads=$3 connections=$4 duration=$5 out=$6
  local -a args=(wrk -t"$threads" -c"$connections" -d"$duration" --latency)
  [ -n "$script" ] && args+=(-s "$script")
  args+=("$url")
  taskset -c "$CLIENT_CPUS" "${args[@]}" | tee "$out"
  WRK_LAST_RPS="$(awk '/Requests\/sec/{print $2}' "$out")"
}

# Best of $reps c50 samples. Sets BEST_RPS.
measure_c50() { # url [lua-script]
  local url=$1 script=${2:-}
  best_rps "$REPS" "$url" "$script" "$C50_THREADS" "$C50_CONNECTIONS" "$C50_DURATION"
}

# Best of $reps c300 samples. Sets BEST_RPS.
measure_c300() { # url [lua-script]
  local url=$1 script=${2:-}
  best_rps "$REPS" "$url" "$script" "$C300_THREADS" "$C300_CONNECTIONS" "$C300_DURATION"
}

best_rps() { # reps url script threads connections duration -> BEST_RPS
  local reps=$1 url=$2 script=$3 threads=$4 connections=$5 duration=$6
  local best=0 i out
  out="$(mktemp)"
  for ((i = 1; i <= reps; i++)); do
    log "sample ${i}/${reps}: wrk -t${threads} -c${connections} -d${duration}${script:+ -s ${script}} ${url}"
    wrk_sample "$url" "$script" "$threads" "$connections" "$duration" "$out"
    if awk -v a="${WRK_LAST_RPS:-0}" -v b="$best" 'BEGIN{exit !(a>b)}'; then
      best="$WRK_LAST_RPS"
    fi
  done
  rm -f "$out"
  BEST_RPS="$best"
}

# Best of $reps c300 samples with concurrent `docker stats` memory sampling.
# Sets BEST_RPS and MEM_PEAK (MiB).
measure_c300_mem() { # container-name url [lua-script]
  local name=$1 url=$2 script=${3:-}
  local best=0 peak=0 i out memout run_peak
  out="$(mktemp)"
  memout="$(mktemp)"
  for ((i = 1; i <= REPS; i++)); do
    log "c300 sample ${i}/${REPS}: wrk -t${C300_THREADS} -c${C300_CONNECTIONS} -d${C300_DURATION}${script:+ -s ${script}} ${url}"
    : > "$memout"
    local -a args=(wrk -t"$C300_THREADS" -c"$C300_CONNECTIONS" -d"$C300_DURATION" --latency)
    [ -n "$script" ] && args+=(-s "$script")
    args+=("$url")
    taskset -c "$CLIENT_CPUS" "${args[@]}" > "$out" 2>&1 &
    local wp=$!
    while kill -0 "$wp" 2>/dev/null; do
      docker stats --no-stream --format '{{.MemUsage}}' "$name" >> "$memout" 2>/dev/null || true
      sleep "$MEM_SAMPLE_INTERVAL"
    done
    wait "$wp" 2>/dev/null || true
    cat "$out"
    WRK_LAST_RPS="$(awk '/Requests\/sec/{print $2}' "$out")"
    if awk -v a="${WRK_LAST_RPS:-0}" -v b="$best" 'BEGIN{exit !(a>b)}'; then
      best="$WRK_LAST_RPS"
    fi
    run_peak="$(mem_peak "$memout")"
    if awk -v a="${run_peak:-0}" -v b="$peak" 'BEGIN{exit !(a>b)}'; then
      peak="$run_peak"
    fi
  done
  rm -f "$out" "$memout"
  BEST_RPS="$best"
  MEM_PEAK="$peak"
}

mem_peak() { # docker-stats-file -> peak MiB
  awk -F'[ /]+' '{ v=$1; if ($0 ~ /GiB/) v=v*1024; gsub(/MiB/,"",v); if (v+0>m) m=v+0 } END{printf "%.1f", m+0}' "$1"
}

# --- reporting -----------------------------------------------------------------

# Append to RESULTS when set, otherwise print. Never point RESULTS at the
# committed results/summary.csv.
report() {
  if [ -n "${RESULTS:-}" ]; then
    case "$RESULTS" in
      */summary.csv | summary.csv)
        die "refusing to write to a committed results file: ${RESULTS}"
        ;;
    esac
    printf '%s\n' "$*" | tee -a "$RESULTS"
  else
    printf '%s\n' "$*"
  fi
}

print_config() {
  cat >&2 <<EOF
benchmark: ${PREFIX}
network:   ${NET}
cpus:      gateway=${GATEWAY_CPUS} client=${CLIENT_CPUS} backend=${BACKEND_CPUS}
memory:    ${GATEWAY_MEMORY}
profile:   c50=-t${C50_THREADS} -c${C50_CONNECTIONS} -d${C50_DURATION}  c300=-t${C300_THREADS} -c${C300_CONNECTIONS} -d${C300_DURATION}
warm-up:   ${WARMUP_DURATION}  reps: ${REPS}
EOF
}
