#!/usr/bin/env bash
# Authentication cost: built-in JWT versus external synchronous auth.
#
# Reproduces the auth rows of results/summary.csv. Every scenario is measured at
# c300 (best-of-five) together with its own no-auth baseline, so the reported
# delta never mixes scenario baselines:
#   api-gateway  baseline vs built-in JWT (in-process HS256)
#   envoy        baseline vs jwt_authn (local JWKS)
#   kong         baseline vs jwt plugin
#   nginx        baseline vs auth_request (external synchronous hop)
#   traefik      baseline vs forwardAuth (external synchronous hop)
#
# Usage: run-auth.sh [--help] [--preflight]
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
. "${SCRIPT_DIR}/common.sh"

usage() {
  cat <<EOF
Usage: $(basename "$0") [--help] [--preflight]

Measures built-in JWT and external-auth scenarios at c300 (best-of-${REPS}) with
their own no-auth baselines. The external auth service listens on :80
(traefik/whoami), matching the committed nginx/traefik configs.

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

JWT_LUA="$(ensure_lua jwt)"

run_c300() { # label [lua-script]
  local label=$1 script=${2:-} url
  wait_gateway "$label"
  url="$(gateway_url)"
  warmup "$url" "$script"
  measure_c300 "$url" "$script"
  report "${label} c300=${BEST_RPS}"
  gw_down
}

# api-gateway
start_gateway_api "${CONFIG_DIR}/api-gateway/baseline.yaml"
run_c300 "api-gateway no-auth"
start_gateway_api "${CONFIG_DIR}/api-gateway/jwt.yaml"
run_c300 "api-gateway JWT" "$JWT_LUA"

# Envoy
start_gateway_envoy "${CONFIG_DIR}/envoy/baseline.yaml"
run_c300 "envoy no-auth"
start_gateway_envoy "${CONFIG_DIR}/envoy/jwt.yaml"
run_c300 "envoy + jwt_authn" "$JWT_LUA"

# Kong
start_gateway_kong "${CONFIG_DIR}/kong/baseline.yaml"
run_c300 "kong no-auth"
start_gateway_kong "${CONFIG_DIR}/kong/jwt.yaml"
run_c300 "kong jwt" "$JWT_LUA"

# Nginx (external auth_request)
start_auth
start_gateway_nginx "${CONFIG_DIR}/nginx/baseline.conf"
run_c300 "nginx no-auth"
start_gateway_nginx "${CONFIG_DIR}/nginx/external-auth.conf"
run_c300 "nginx + auth_request"

# Traefik (external forwardAuth)
start_gateway_traefik "${CONFIG_DIR}/traefik/static.yaml" "${CONFIG_DIR}/traefik/dynamic.yaml"
run_c300 "traefik no-auth"
start_gateway_traefik "${CONFIG_DIR}/traefik/static.yaml" "${CONFIG_DIR}/traefik/dynamic-forward-auth.yaml"
run_c300 "traefik forwardauth"

log "DONE"
