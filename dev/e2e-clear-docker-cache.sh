#!/usr/bin/env bash
set -euo pipefail

# Clears docker build cache used by e2e tests.
# Useful when you need to force a fresh build without cached dependencies.
#
# Usage:
#   dev/e2e-clear-docker-cache.sh        # Clear cache mounts only (default)
#   dev/e2e-clear-docker-cache.sh all    # Clear all build cache

mode=${1:-mounts}

set -x

case "$mode" in
    mounts)
        # Clear only cache mounts (cargo registry, target dirs, etc.)
        docker builder prune --filter type=exec.cachemount -f
        ;;
    all)
        # Clear all build cache including layers
        docker builder prune -a -f
        ;;
    *)
        echo "Usage: $0 [mounts|all]" >&2
        exit 1
        ;;
esac
