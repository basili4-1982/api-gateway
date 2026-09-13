#!/usr/bin/env bash
# Remove benchmark resources created by the runners.
#
# Cleanup is scoped to the benchmark prefix: only containers whose name starts
# with "${PREFIX}-" and the "${PREFIX}-net" network are touched. Nothing else on
# the host is modified.
#
# Usage: cleanup.sh [--help] [--dry-run]
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
. "${SCRIPT_DIR}/common.sh"

usage() {
  cat <<EOF
Usage: $(basename "$0") [--help] [--dry-run]

Removes containers named "\${PREFIX}-*" and the "\${PREFIX}-net" network created
by the benchmark runners. Override PREFIX to target a different campaign.

Options:
  --help, -h    show this help and exit
  --dry-run     list the resources that would be removed and exit
EOF
}

case "${1:-}" in
  --help | -h)
    usage
    exit 0
    ;;
  --dry-run)
    log "containers with prefix ${PREFIX}-:"
    docker ps -a --format '  {{.Names}}' --filter "name=${PREFIX}-" 2>/dev/null || true
    if docker network inspect "$NET" >/dev/null 2>&1; then
      log "network: ${NET}"
    else
      log "network ${NET} not present"
    fi
    exit 0
    ;;
  "")
    ;;
  *)
    die "unknown argument: $1 (try --help)"
    ;;
esac

cleanup_benchmark
log "cleanup complete"
