#!/usr/bin/env bash
# Build a goreleaser snapshot in a linux/amd64 Docker container.
#
# Usage: ./scripts/build-snapshot.sh <env> <config-name> [--quiet]
#
# Examples:
#   ./scripts/build-snapshot.sh devnet controller
#   ./scripts/build-snapshot.sh testnet client --quiet
#
# The <env> and <config-name> correspond to a goreleaser config file at:
#   release/.goreleaser.<env>.<config-name>.yaml
#
# Requirements: docker
# Environment:  GORELEASER_KEY (goreleaser pro license key)
#
# Artifacts are written to the dist/ directory.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# --- Formatting ---

BOLD='\033[1m' DIM='\033[2m' GREEN='\033[0;32m'
RED='\033[0;31m' CYAN='\033[0;36m' RESET='\033[0m'
if [[ ! -t 1 ]]; then BOLD="" DIM="" GREEN="" RED="" CYAN="" RESET=""; fi

die() { echo -e "${RED}ERROR:${RESET} $*" >&2; exit 1; }

# --- Args ---

QUIET=false
ENV_NAME=""
CONFIG_NAME=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --quiet|-q) QUIET=true; shift ;;
        --help|-h)
            echo "Usage: $0 <env> <config-name> [--quiet]"
            echo ""
            echo "Build a goreleaser snapshot in a linux/amd64 Docker container."
            echo "Artifacts are written to the dist/ directory."
            echo ""
            echo "Arguments:"
            echo "  <env>           Environment: devnet or testnet"
            echo "  <config-name>   Name of the goreleaser config (e.g. controller, client)"
            echo "                  Must match: release/.goreleaser.<env>.<config-name>.yaml"
            echo ""
            echo "Available configs:"
            echo "  devnet:"
            ls "$REPO_ROOT"/release/.goreleaser.devnet.*.yaml 2>/dev/null \
                | sed 's|.*/\.goreleaser\.devnet\.||; s|\.yaml$||; s|^|    |'
            echo "  testnet:"
            ls "$REPO_ROOT"/release/.goreleaser.testnet.*.yaml 2>/dev/null \
                | sed 's|.*/\.goreleaser\.testnet\.||; s|\.yaml$||; s|^|    |'
            echo ""
            echo "Flags:"
            echo "  --quiet, -q   Suppress verbose goreleaser output"
            echo "  --help        Show this help"
            echo ""
            echo "Environment:"
            echo "  GORELEASER_KEY   Goreleaser Pro license key (required)"
            exit 0
            ;;
        -*)  die "Unknown flag: $1" ;;
        *)
            if [[ -z "$ENV_NAME" ]]; then
                ENV_NAME="$1"; shift
            elif [[ -z "$CONFIG_NAME" ]]; then
                CONFIG_NAME="$1"; shift
            else
                die "Unexpected argument: $1"
            fi
            ;;
    esac
done

[[ -n "$ENV_NAME" ]] || die "Missing required argument: <env>\nRun '$0 --help' for usage."
[[ "$ENV_NAME" == "devnet" || "$ENV_NAME" == "testnet" ]] || die "Invalid env: $ENV_NAME (must be devnet or testnet)"
[[ -n "$CONFIG_NAME" ]] || die "Missing required argument: <config-name>\nRun '$0 --help' for usage."

# --- Preflight ---

command -v docker >/dev/null 2>&1 || die "docker is required"
[[ -n "${GORELEASER_KEY:-}" ]] || die "GORELEASER_KEY environment variable is required"

cd "$REPO_ROOT"

GORELEASER_CONFIG="release/.goreleaser.${ENV_NAME}.${CONFIG_NAME}.yaml"
[[ -f "$GORELEASER_CONFIG" ]] || die "Config not found: $GORELEASER_CONFIG"

# Extract project name from the base config (included by the env config).
BASE_CONFIG="release/.goreleaser.base.${CONFIG_NAME}.yaml"
PROJECT_NAME=""
if [[ -f "$BASE_CONFIG" ]]; then
    PROJECT_NAME=$(grep 'project_name:' "$BASE_CONFIG" | head -1 | sed 's/.*project_name: *//' | tr -d '[:space:]')
fi
[[ -n "$PROJECT_NAME" ]] || PROJECT_NAME="$CONFIG_NAME"
SHORT_COMMIT=$(git rev-parse --short HEAD)

echo ""
echo -e "${BOLD}${PROJECT_NAME} snapshot build (${ENV_NAME})${RESET}"
echo -e "  Commit:   ${DIM}${SHORT_COMMIT}${RESET}"
echo ""

# --- Build in Docker ---

RELEASE_IMAGE="doublezero-release"
DOCKERFILE="release/Dockerfile.release"
DOCKERFILE_HASH=$(shasum -a 256 "$DOCKERFILE" | cut -d' ' -f1 | head -c 12)

# Rebuild the Docker image if it doesn't exist or the Dockerfile has changed.
EXISTING_HASH=$(docker image inspect "$RELEASE_IMAGE" --format '{{index .Config.Labels "dockerfile.hash"}}' 2>/dev/null || true)
if [[ "$EXISTING_HASH" != "$DOCKERFILE_HASH" ]]; then
    if [[ -n "$EXISTING_HASH" ]]; then
        echo -e "${BOLD}${CYAN}[0/1]${RESET} ${BOLD}Rebuilding release Docker image (Dockerfile changed)${RESET}"
    else
        echo -e "${BOLD}${CYAN}[0/1]${RESET} ${BOLD}Building release Docker image${RESET}"
    fi
    echo ""
    docker build --platform linux/amd64 \
        -t "$RELEASE_IMAGE" \
        --label "dockerfile.hash=$DOCKERFILE_HASH" \
        -f "$DOCKERFILE" \
        release/
    echo ""
fi

# goreleaser writes artifacts to dist/ by default.
DIST_DIR="$REPO_ROOT/dist"
rm -rf "$DIST_DIR"

VERBOSE_FLAG="--verbose"
if [[ "$QUIET" == true ]]; then
    VERBOSE_FLAG=""
fi

echo -e "${BOLD}${CYAN}[1/1]${RESET} ${BOLD}Building snapshot in linux/amd64 container${RESET}"
echo ""

# Use named volumes for cargo registry and build cache so subsequent builds
# are fast. The release image has rust + go + goreleaser-pro pre-installed.
CONTAINER_NAME="doublezero-release-$$"
trap 'docker rm -f "$CONTAINER_NAME" 2>/dev/null; exit 130' INT TERM

docker run --rm --init \
    --name "$CONTAINER_NAME" \
    --platform linux/amd64 \
    -v "$REPO_ROOT":/workspace \
    -v doublezero-cargo-registry:/usr/local/cargo/registry \
    -v doublezero-cargo-git:/usr/local/cargo/git \
    -v "doublezero-cargo-target-${CONFIG_NAME}:/workspace/target" \
    -v doublezero-go-cache:/root/go \
    -w /workspace \
    -e "GORELEASER_KEY=${GORELEASER_KEY}" \
    "$RELEASE_IMAGE" \
    bash -c "
        set -euo pipefail

        goreleaser release \
            -f ${GORELEASER_CONFIG} \
            --snapshot \
            --clean \
            ${VERBOSE_FLAG}
    "

trap - INT TERM

echo ""

# List artifacts (debs, rpms, tar.gz, checksums).
ARTIFACTS=()
for f in "$DIST_DIR"/*.deb "$DIST_DIR"/*.rpm "$DIST_DIR"/*.tar.gz "$DIST_DIR"/*checksums*; do
    [[ -f "$f" ]] && ARTIFACTS+=("$f")
done

if [[ ${#ARTIFACTS[@]} -eq 0 ]]; then
    die "No artifacts found in $DIST_DIR"
fi

echo -e "  ${GREEN}✓${RESET} Build complete"
echo ""
for f in "${ARTIFACTS[@]}"; do
    echo -e "  ${DIM}$(basename "$f")${RESET}"
done
echo ""
echo -e "Artifacts in: ${DIM}${DIST_DIR}${RESET}"
echo ""
