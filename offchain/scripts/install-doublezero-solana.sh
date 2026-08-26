#!/usr/bin/env bash
# Install doublezero-solana from GitHub Releases.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/malbeclabs/doublezero-offchain/main/scripts/install-doublezero-solana.sh | sudo bash
#   curl -fsSL https://raw.githubusercontent.com/malbeclabs/doublezero-offchain/main/scripts/install-doublezero-solana.sh | bash -s -- --version 0.4.2-rc4
#   curl -fsSL https://raw.githubusercontent.com/malbeclabs/doublezero-offchain/main/scripts/install-doublezero-solana.sh | bash -s -- --install-dir .
#   curl -fsSL https://raw.githubusercontent.com/malbeclabs/doublezero-offchain/main/scripts/install-doublezero-solana.sh | bash -s -- --format tar.gz
#
# By default, installs via .deb on Debian/Ubuntu, .rpm on RHEL/Fedora, or
# .tar.gz otherwise. Use --install-dir to extract the binary to a custom
# location instead of using a package manager.

set -euo pipefail

REPO="malbeclabs/doublezero-offchain"
TAG_PREFIX="doublezero-solana"

# --- Formatting ---

BOLD='\033[1m' DIM='\033[2m' GREEN='\033[0;32m'
RED='\033[0;31m' YELLOW='\033[0;33m' RESET='\033[0m'
if [[ ! -t 1 ]]; then BOLD="" DIM="" GREEN="" RED="" YELLOW="" RESET=""; fi

die() { echo -e "${RED}ERROR:${RESET} $*" >&2; exit 1; }
info() { echo -e "${BOLD}$*${RESET}"; }

# --- Args ---

VERSION=""
INSTALL_DIR=""
FORMAT=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --version) [[ -n "${2:-}" ]] || die "--version requires a value"; VERSION="$2"; shift 2 ;;
        --install-dir) [[ -n "${2:-}" ]] || die "--install-dir requires a value"; INSTALL_DIR="$2"; shift 2 ;;
        --format) [[ -n "${2:-}" ]] || die "--format requires a value"; FORMAT="$2"; shift 2 ;;
        --help|-h)
            echo "Usage: $0 [--version <version>] [--install-dir <dir>] [--format <fmt>]"
            echo ""
            echo "Install doublezero-solana from GitHub Releases."
            echo ""
            echo "Flags:"
            echo "  --version <ver>       Version to install (e.g. 0.4.2-rc4); defaults to latest"
            echo "  --install-dir <dir>   Extract binary to this directory (uses tar.gz)"
            echo "  --format <fmt>        Force asset format: deb, rpm, or tar.gz"
            echo "  --help                Show this help"
            exit 0
            ;;
        *) die "Unknown argument: $1" ;;
    esac
done

# --- Preflight ---

command -v curl >/dev/null 2>&1 || die "curl is required"

# --- Detect install method ---

if [[ -n "$INSTALL_DIR" ]]; then
    # --install-dir forces tar.gz extraction.
    METHOD="tar"
elif [[ -n "$FORMAT" ]]; then
    case "$FORMAT" in
        deb) METHOD="deb" ;;
        rpm) METHOD="rpm" ;;
        tar.gz) METHOD="tar" ;;
        *) die "Unknown format: $FORMAT (expected deb, rpm, or tar.gz)" ;;
    esac
elif command -v dpkg >/dev/null 2>&1; then
    METHOD="deb"
elif command -v rpm >/dev/null 2>&1; then
    METHOD="rpm"
else
    METHOD="tar"
fi

# Check if we need root for package manager installs.
if [[ "$METHOD" != "tar" && $(id -u) -ne 0 ]]; then
    echo -e "${YELLOW}Root is required to install via ${METHOD}.${RESET}"
    echo ""
    echo "Options:"
    echo "  sudo $0 $*"
    echo "  $0 $* --install-dir .     # extract binary to current directory"
    echo ""
    exit 1
fi

# Set asset pattern for download.
case "$METHOD" in
    deb) ASSET_PATTERN="\.deb\"" ;;
    rpm) ASSET_PATTERN="\.rpm\"" ;;
    tar) ASSET_PATTERN="\.tar\.gz\"" ;;
esac

# --- Determine version ---

if [[ -z "$VERSION" ]]; then
    info "Finding latest release..."

    # Get the latest release tag (including pre-releases).
    VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases" \
        | grep -o "\"tag_name\": *\"${TAG_PREFIX}/v[^\"]*\"" \
        | head -1 \
        | sed "s|.*${TAG_PREFIX}/v||; s|\"||g")

    [[ -n "$VERSION" ]] || die "Could not find any ${TAG_PREFIX} releases"
fi

TAG="${TAG_PREFIX}/v${VERSION}"

info "Installing doublezero-solana ${VERSION} (via ${METHOD})"
echo ""

# --- Download asset ---

info "Downloading..."

ASSET_URL=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/tags/${TAG}" \
    | grep -o "\"browser_download_url\": *\"[^\"]*${ASSET_PATTERN}" \
    | head -1 \
    | sed 's/"browser_download_url": *"//; s/"$//')

[[ -n "$ASSET_URL" ]] || die "No ${METHOD} asset found for ${TAG}"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

ASSET_FILE="${TMPDIR}/$(basename "$ASSET_URL")"
curl -fsSL -o "$ASSET_FILE" "$ASSET_URL"

echo -e "  ${DIM}$(basename "$ASSET_URL")${RESET}"
echo ""

# --- Install ---

info "Installing..."
echo ""

case "$METHOD" in
    deb)
        dpkg -i "$ASSET_FILE"
        ;;
    rpm)
        rpm -U --force "$ASSET_FILE"
        ;;
    tar)
        [[ -z "$INSTALL_DIR" ]] && INSTALL_DIR="."
        mkdir -p "$INSTALL_DIR"
        tar -xzf "$ASSET_FILE" -C "$TMPDIR"
        BINARY=$(find "$TMPDIR" -name "doublezero-solana" -type f | head -1)
        [[ -n "$BINARY" ]] || die "Could not find doublezero-solana binary in archive"
        install -m 755 "$BINARY" "${INSTALL_DIR}/doublezero-solana"
        echo -e "  ${DIM}Installed to ${INSTALL_DIR}/doublezero-solana${RESET}"
        ;;
esac

echo ""
echo -e "${GREEN}${BOLD}doublezero-solana ${VERSION} installed!${RESET}"
echo ""

doublezero-solana --version 2>/dev/null || true
