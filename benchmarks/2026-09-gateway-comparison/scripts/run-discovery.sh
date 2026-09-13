#!/usr/bin/env bash
# Docker service-discovery reaction latency.
#
# Reproduces the discovery rows of results/summary.csv. A labelled whoami "pod"
# is created and destroyed while the gateway discovers it through the Docker
# socket. Three timings are reported per cycle:
#   up    time until the first 200 on /pod after the container starts
#   fail  time until the first non-200 after the container stops
#   gone  time until the route returns 404 after the container stops
#
# Usage: run-discovery.sh [--help] [--preflight]
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
. "${SCRIPT_DIR}/common.sh"

DISCOVERY_REPS="${DISCOVERY_REPS:-3}"

usage() {
  cat <<EOF
Usage: $(basename "$0") [--help] [--preflight]

Measures api-gateway and traefik Docker service-discovery latency over
${DISCOVERY_REPS} create/destroy cycles each.

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

start_api_discovery() {
  docker rm -f "$GW_CONTAINER" >/dev/null 2>&1 || true
  docker run -d --name "$GW_CONTAINER" --network "$NET" \
    --cpuset-cpus="$GATEWAY_CPUS" --memory="$GATEWAY_MEMORY" \
    -v /var/run/docker.sock:/var/run/docker.sock:ro \
    -v "${CONFIG_DIR}/api-gateway/discovery.yaml:/etc/proxy/config.yaml:ro" \
    "$GATEWAY_IMAGE" >/dev/null || die "cannot start api-gateway discovery"
  GW_PORT=8080
}

start_traefik_discovery() {
  docker rm -f "$GW_CONTAINER" >/dev/null 2>&1 || true
  docker run -d --name "$GW_CONTAINER" --network "$NET" \
    --cpuset-cpus="$GATEWAY_CPUS" --memory="$GATEWAY_MEMORY" \
    -v /var/run/docker.sock:/var/run/docker.sock:ro \
    -v "${CONFIG_DIR}/traefik/docker-provider.yaml:/etc/traefik/traefik.yml:ro" \
    "$TRAEFIK_IMAGE" >/dev/null || die "cannot start traefik discovery"
  GW_PORT=80
}

measure() { # label ip port path
  local label=$1 ip=$2 port=$3 path=$4
  local url="http://${ip}:${port}${path}"
  local t0 t2 code up=0 fail=0 gone=0
  docker rm -f "$POD_CONTAINER" >/dev/null 2>&1 || true
  sleep 1
  t0="$(date +%s%3N)"
  docker run -d --name "$POD_CONTAINER" --network "$NET" --cpuset-cpus="$BACKEND_CPUS" \
    --label gateway.enable=true --label gateway.name=pod \
    --label gateway.port=80 --label gateway.path_prefix=/pod \
    --label traefik.enable=true \
    --label 'traefik.http.routers.pod.rule=PathPrefix(`/pod`)' \
    --label traefik.http.services.pod.loadbalancer.server.port=80 \
    "$BACKEND_IMAGE" >/dev/null || die "cannot start discovery pod"

  for _ in $(seq 1 600); do
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 1 "$url")"
    if [ "$code" = "200" ]; then
      up=$(( $(date +%s%3N) - t0 ))
      break
    fi
    sleep 0.03
  done

  t2="$(date +%s%3N)"
  docker stop "$POD_CONTAINER" >/dev/null 2>&1 || true
  for _ in $(seq 1 600); do
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 1 "$url")"
    if [ "$code" != "200" ] && [ "$fail" = 0 ]; then
      fail=$(( $(date +%s%3N) - t2 ))
    fi
    if [ "$code" = "404" ]; then
      gone=$(( $(date +%s%3N) - t2 ))
      break
    fi
    sleep 0.03
  done

  report "${label} up=${up}ms fail=${fail}ms gone=${gone}ms"
  docker rm -f "$POD_CONTAINER" >/dev/null 2>&1 || true
}

run_discovery() { # label
  local label=$1 ip i
  wait_gateway "$label"
  sleep 1
  ip="$(docker_ip "$GW_CONTAINER")"
  for ((i = 1; i <= DISCOVERY_REPS; i++)); do
    log "${label}: discovery cycle ${i}/${DISCOVERY_REPS}"
    measure "$label" "$ip" "$GW_PORT" /pod
    sleep 2
  done
  gw_down
}

start_api_discovery
run_discovery api-gateway

start_traefik_discovery
run_discovery traefik

log "DONE"
