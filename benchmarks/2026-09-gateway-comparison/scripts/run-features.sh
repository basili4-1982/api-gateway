#!/usr/bin/env bash
# Incremental api-gateway feature costs.
#
# Reproduces the feature-cost rows of results/summary.csv. Two baselines are
# measured first (no-auth and built-in JWT) so each feature can be compared with
# the correct baseline. Scenarios:
#   rbac                  role check from JWT claims (admin route)
#   rate-limit            token-bucket limiter with effectively unlimited rate
#   permission-cache      external permission service behind a 300s TTL cache
#   permission-no-cache   external permission lookup on every request
#   webhook-http-direct   one async HTTP POST per request (no batching)
#   webhook-http-batched  bounded queue, batches of 1000 or 100ms
#   webhook-nats          async publish to NATS
#
# Usage: run-features.sh [--help] [--preflight]
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
. "${SCRIPT_DIR}/common.sh"

usage() {
  cat <<EOF
Usage: $(basename "$0") [--help] [--preflight]

Measures api-gateway feature scenarios at c300 (best-of-${REPS}) plus the no-auth
and JWT baselines. RBAC and permission scenarios include a correctness smoke
check; HTTP webhook scenarios report how many requests reached the sink.

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
    require_cmd go
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
require_cmd go
print_config
ensure_gateway_image
start_network
trap_benchmark_cleanup
start_backend

JWT_LUA="$(ensure_lua jwt)"
RBAC_LUA="$(ensure_lua rbac)"
PERMS_LUA="$(ensure_lua perms)"
RBAC_TOKEN="$(sed -n 's/.*Bearer \(.*\)"/\1/p' "$RBAC_LUA")"
PERMS_TOKEN="$(sed -n 's/.*Bearer \(.*\)"/\1/p' "$PERMS_LUA")"

run_c300() { # label [lua-script]
  local label=$1 script=${2:-} url
  wait_gateway "$label"
  url="$(gateway_url)"
  warmup "$url" "$script"
  measure_c300 "$url" "$script"
  report "${label} c300=${BEST_RPS}"
  gw_down
}

smoke_status() { # url [token]
  local url=$1 token=${2:-}
  if [ -n "$token" ]; then
    curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${token}" "$url"
  else
    curl -s -o /dev/null -w '%{http_code}' "$url"
  fi
}

# --- baselines -----------------------------------------------------------------

start_gateway_api "${CONFIG_DIR}/api-gateway/baseline.yaml"
run_c300 "api-gateway no-auth"
start_gateway_api "${CONFIG_DIR}/api-gateway/jwt.yaml"
run_c300 "api-gateway JWT" "$JWT_LUA"

# --- RBAC ----------------------------------------------------------------------

start_gateway_api "${CONFIG_DIR}/api-gateway/rbac.yaml"
wait_gateway "api-gateway RBAC"
RBAC_URL="$(gateway_url)"
log "rbac smoke: no-token=$(smoke_status "$RBAC_URL") admin=$(smoke_status "$RBAC_URL" "$RBAC_TOKEN")"
warmup "$RBAC_URL" "$RBAC_LUA"
measure_c300 "$RBAC_URL" "$RBAC_LUA"
report "api-gateway RBAC c300=${BEST_RPS}"
gw_down

# --- route rate limit ----------------------------------------------------------

start_gateway_api "${CONFIG_DIR}/api-gateway/ratelimit.yaml"
run_c300 "api-gateway rate-limit"

# --- permission service --------------------------------------------------------

start_perms
start_gateway_api "${CONFIG_DIR}/api-gateway/permissions-cache.yaml"
wait_gateway "api-gateway perms-cache"
PERMS_URL="$(gateway_url)"
log "perms-cache smoke: $(curl -s -H "Authorization: Bearer ${PERMS_TOKEN}" "$PERMS_URL" | grep -o 'X-User-Permissions: [a-z,]*' || true)"
warmup "$PERMS_URL" "$PERMS_LUA"
measure_c300 "$PERMS_URL" "$PERMS_LUA"
report "api-gateway perms-cache c300=${BEST_RPS}"
gw_down

start_gateway_api "${CONFIG_DIR}/api-gateway/permissions-no-cache.yaml"
wait_gateway "api-gateway perms-no-cache"
PERMS_URL="$(gateway_url)"
log "perms-no-cache smoke: $(curl -s -H "Authorization: Bearer ${PERMS_TOKEN}" "$PERMS_URL" | grep -o 'X-User-Permissions: [a-z,]*' || true)"
warmup "$PERMS_URL" "$PERMS_LUA"
measure_c300 "$PERMS_URL" "$PERMS_LUA"
report "api-gateway perms-no-cache c300=${BEST_RPS}"
gw_down

# --- HTTP webhooks -------------------------------------------------------------

start_sink
start_gateway_api "${CONFIG_DIR}/api-gateway/webhook-direct.yaml"
run_c300 "api-gateway webhook-http-direct"
report "webhook-http-direct sink requests seen: $(docker logs "$SINK_CONTAINER" 2>&1 | wc -l)"

start_sink
start_gateway_api "${CONFIG_DIR}/api-gateway/webhook-batch.yaml"
run_c300 "api-gateway webhook-http-batched"
report "webhook-http-batched sink requests seen: $(docker logs "$SINK_CONTAINER" 2>&1 | wc -l)"

# --- NATS webhook --------------------------------------------------------------

start_nats
start_gateway_api "${CONFIG_DIR}/api-gateway/webhook-nats.yaml"
run_c300 "api-gateway webhook-nats"

log "DONE"
