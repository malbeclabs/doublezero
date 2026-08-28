#!/usr/bin/env bash
# Build a goreleaser snapshot in a linux/amd64 Docker container and publish it
# as a release candidate on GitHub Releases.
#
# Usage: ./scripts/release-rc.sh <config-name> [--version <version>] [--dry-run]
#
# Examples:
#   ./scripts/release-rc.sh doublezero-solana-cli                  # continues latest RC series
#   ./scripts/release-rc.sh doublezero-solana-cli --version 0.4.2  # starts or continues 0.4.2 RCs
#   ./scripts/release-rc.sh sentinel --version 0.2.6 --dry-run
#
# The <config-name> corresponds to a goreleaser config file at:
#   release/.goreleaser.<config-name>.yaml
#
# Requirements: docker, gh (GitHub CLI, authenticated)
# Environment:  GORELEASER_KEY (goreleaser pro license key)
#
# The script will:
#   1. Parse tag prefix from the goreleaser config
#   2. Determine the next RC number from existing GitHub releases
#   3. Run goreleaser snapshot inside a linux/amd64 container
#   4. Upload the artifacts as a pre-release to GitHub

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# --- Formatting ---

BOLD='\033[1m' DIM='\033[2m' GREEN='\033[0;32m' YELLOW='\033[0;33m'
RED='\033[0;31m' CYAN='\033[0;36m' RESET='\033[0m'
if [[ ! -t 1 ]]; then BOLD="" DIM="" GREEN="" YELLOW="" RED="" CYAN="" RESET=""; fi

die() { echo -e "${RED}ERROR:${RESET} $*" >&2; exit 1; }

# --- Args ---

DRY_RUN=false
QUIET=false
CONFIG_NAME=""
BASE_VERSION=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run) DRY_RUN=true; shift ;;
        --quiet|-q) QUIET=true; shift ;;
        --version) [[ -n "${2:-}" ]] || die "--version requires a value"; BASE_VERSION="$2"; shift 2 ;;
        --help|-h)
            echo "Usage: $0 <config-name> [--version <version>] [--dry-run]"
            echo ""
            echo "Build a goreleaser snapshot in a linux/amd64 Docker container"
            echo "and publish it as a release candidate on GitHub."
            echo ""
            echo "Arguments:"
            echo "  <config-name>   Name of the goreleaser config (e.g. doublezero-solana-cli)"
            echo "                  Must match: release/.goreleaser.<config-name>.yaml"
            echo ""
            echo "Available configs:"
            ls release/.goreleaser.*.yaml 2>/dev/null | sed 's|.*/\.goreleaser\.||; s|\.yaml$||; s|^|    |'
            echo ""
            echo "Flags:"
            echo "  --version <ver>   Base version for the RC (e.g. 0.4.2); defaults to latest RC series"
            echo "  --quiet, -q       Suppress verbose goreleaser output"
            echo "  --dry-run         Build only, skip publishing to GitHub"
            echo "  --help            Show this help"
            echo ""
            echo "Environment:"
            echo "  GORELEASER_KEY   Goreleaser Pro license key (required)"
            exit 0
            ;;
        -*)  die "Unknown flag: $1" ;;
        *)
            [[ -n "$CONFIG_NAME" ]] && die "Unexpected argument: $1"
            CONFIG_NAME="$1"; shift
            ;;
    esac
done

[[ -n "$CONFIG_NAME" ]] || die "Missing required argument: <config-name>\nRun '$0 --help' for usage."

# --- Preflight ---

command -v docker >/dev/null 2>&1 || die "docker is required"
command -v gh >/dev/null 2>&1 || die "gh (GitHub CLI) is required"
[[ -n "${GORELEASER_KEY:-}" ]] || die "GORELEASER_KEY environment variable is required"

cd "$REPO_ROOT"

GORELEASER_CONFIG="release/.goreleaser.${CONFIG_NAME}.yaml"
[[ -f "$GORELEASER_CONFIG" ]] || die "Config not found: $GORELEASER_CONFIG"

# --- Parse goreleaser config ---

# Extract the tag prefix (e.g. "doublezero-solana/").
TAG_PREFIX=$(grep 'tag_prefix:' "$GORELEASER_CONFIG" | head -1 | sed 's/.*tag_prefix: *//' | tr -d '[:space:]')
[[ -n "$TAG_PREFIX" ]] || die "Could not parse tag_prefix from $GORELEASER_CONFIG"

# --- Determine version and RC number ---

# If no --version given, infer from the latest RC release on GitHub.
if [[ -z "$BASE_VERSION" ]]; then
    LATEST_RC_TAG=$(gh release list --limit 50 \
        | grep "${TAG_PREFIX}v.*-rc" \
        | head -1 \
        | sed "s|.*${TAG_PREFIX}v\([0-9.]*\)-rc.*|\1|" || true)

    if [[ -n "$LATEST_RC_TAG" ]]; then
        BASE_VERSION="$LATEST_RC_TAG"
    else
        die "No existing RC releases found for ${TAG_PREFIX}. Use --version to specify."
    fi
fi

# Find next RC number by inspecting existing releases.
LAST_RC=$(gh release list --limit 50 \
    | grep "${TAG_PREFIX}v${BASE_VERSION}-rc" \
    | head -1 \
    | sed "s|.*v${BASE_VERSION}-rc\([0-9]*\).*|\1|" || true)

if [[ -n "$LAST_RC" ]]; then
    NEXT_RC=$((LAST_RC + 1))
else
    NEXT_RC=1
fi

RC_VERSION="${BASE_VERSION}-rc${NEXT_RC}"
RC_TAG="${TAG_PREFIX}v${RC_VERSION}"
SHORT_COMMIT=$(git rev-parse --short HEAD)

# Extract project name for display.
PROJECT_NAME=$(grep 'project_name:' "$GORELEASER_CONFIG" | head -1 | sed 's/.*project_name: *//' | tr -d '[:space:]')
[[ -n "$PROJECT_NAME" ]] || PROJECT_NAME="$CONFIG_NAME"

echo ""
echo -e "${BOLD}${PROJECT_NAME} release candidate${RESET}"
echo -e "  Version:  ${DIM}${RC_VERSION}${RESET}"
echo -e "  Tag:      ${DIM}${RC_TAG}${RESET}"
echo -e "  Commit:   ${DIM}${SHORT_COMMIT}${RESET}"
if [[ "$DRY_RUN" == true ]]; then
    echo -e "  Mode:     ${YELLOW}DRY RUN (build only, no publish)${RESET}"
fi
echo ""

read -rp "Press Enter to continue, or Ctrl-C to abort... "
echo ""

# --- Build in Docker ---

RELEASE_IMAGE="doublezero-release"

# Build the release Docker image if it doesn't exist or if --no-cache is desired.
if ! docker image inspect "$RELEASE_IMAGE" >/dev/null 2>&1; then
    echo -e "${BOLD}${CYAN}[0/2]${RESET} ${BOLD}Building release Docker image${RESET}"
    echo ""
    docker build --platform linux/amd64 \
        -t "$RELEASE_IMAGE" \
        -f release/Dockerfile.release \
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

echo -e "${BOLD}${CYAN}[1/2]${RESET} ${BOLD}Building snapshot in linux/amd64 container${RESET}"
echo ""

# Use named volumes for cargo registry and build cache so subsequent builds
# are fast. The release image has rust + goreleaser-pro + rpm pre-installed.
CONTAINER_NAME="doublezero-release-$$"
trap 'docker rm -f "$CONTAINER_NAME" 2>/dev/null; exit 130' INT TERM

docker run --rm --init \
    --name "$CONTAINER_NAME" \
    --platform linux/amd64 \
    -v "$REPO_ROOT":/workspace \
    -v doublezero-cargo-registry:/usr/local/cargo/registry \
    -v doublezero-cargo-git:/usr/local/cargo/git \
    -v "doublezero-cargo-target-${CONFIG_NAME}:/workspace/target" \
    -w /workspace \
    -e "GORELEASER_KEY=${GORELEASER_KEY}" \
    -e "GORELEASER_CURRENT_TAG=${RC_TAG}" \
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

# Collect uploadable artifacts (debs, rpms, tar.gz, checksums).
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

# --- Publish ---

if [[ "$DRY_RUN" == true ]]; then
    echo ""
    echo -e "${YELLOW}Dry run — skipping publish. Artifacts in:${RESET} $DIST_DIR"
    echo ""
    exit 0
fi

echo ""
echo -e "${BOLD}${CYAN}[2/2]${RESET} ${BOLD}Publishing ${RC_TAG} to GitHub Releases${RESET}"
echo ""

# Create a lightweight tag (-m forces it to skip the editor).
git tag -m "Release candidate ${RC_VERSION}" "$RC_TAG"
git push origin "$RC_TAG"

# Create the release with artifacts.
gh release create "$RC_TAG" \
    --prerelease \
    --title "$RC_TAG" \
    --notes "Release candidate \`${RC_VERSION}\` built from commit \`${SHORT_COMMIT}\`." \
    "${ARTIFACTS[@]}"

RELEASE_URL=$(gh release view "$RC_TAG" --json url --jq '.url')

echo ""
echo -e "${BOLD}${GREEN}Published!${RESET}"
echo -e "  ${DIM}${RELEASE_URL}${RESET}"
echo ""

# Clean up dist directory.
rm -rf "$DIST_DIR"
