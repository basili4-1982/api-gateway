#!/usr/bin/env bash
# Proxy-only throughput (c50/c300) and c300 peak memory for the five gateways.
#
# Reproduces the proxy-only and proxy-memory rows of results/summary.csv. One
# gateway runs at a time on CPUs 0-1 with a 768m limit; the load client runs on
# CPUs 2-7 and the whoami backend on 8-11. Values are best-of-five, memory is the
# peak `docker stats` reading during the c300 samples.
#
# Usage: run-throughput.sh [--help] [--preflight]
# Environment overrides: PREFIX, NET, *_CPUS, GATEWAY_MEMORY, C50_*, C300_*, REPS,
#   WARMUP_DURATION, RESULTS and any *_IMAGE from common.sh.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
. "${SCRIPT_DIR}/common.sh"

usage() {
  cat <<EOF
Usage: $(basename "$0") [--help] [--preflight]

Runs the proxy-only benchmark for api-gateway, traefik, nginx, envoy and kong.
Each gateway is measured with best-of-${REPS} c50 and c300 wrk runs plus c300
peak memory, using the documented September 2026 profile.

Options:
  --help, -h    show this help and exit
  --preflight   check dependencies, print the effective config and exit
EOF
}

case "${1:-}" in
  --help | -h)
    usage
    exit 0
    ;;
  --preflight)
    preflight
    print_config
    exit 0
    ;;
  "")
    ;;
  *)
    die "unknown argument: $1 (try --help)"
    ;;
esac

preflight
print_config
ensure_gateway_image
start_network
trap_benchmark_cleanup
start_backend

run_proxy_gateway() { # label
  local label=$1 url r50 r300 mem
  wait_gateway "$label"
  url="$(gateway_url)"
  warmup "$url"
  measure_c50 "$url"
  r50="$BEST_RPS"
  measure_c300_mem "$GW_CONTAINER" "$url"
  r300="$BEST_RPS"
  mem="$MEM_PEAK"
  report "${label} c50=${r50} c300=${r300} mem_peak=${mem}MB"
  gw_down
}

start_gateway_api "${CONFIG_DIR}/api-gateway/baseline.yaml"
run_proxy_gateway api-gateway

start_gateway_traefik "${CONFIG_DIR}/traefik/static.yaml" "${CONFIG_DIR}/traefik/dynamic.yaml"
run_proxy_gateway traefik

start_gateway_nginx "${CONFIG_DIR}/nginx/baseline.conf"
run_proxy_gateway nginx

start_gateway_envoy "${CONFIG_DIR}/envoy/baseline.yaml"
run_proxy_gateway envoy

start_gateway_kong "${CONFIG_DIR}/kong/baseline.yaml"
run_proxy_gateway kong

log "DONE"
