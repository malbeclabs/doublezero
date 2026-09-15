#!/usr/bin/env bash
#
# Install the platform-tools release that `cargo build-sbf --tools-version` asks
# for, so that the flag is actually honored.
#
# cargo-build-sbf only accepts a --tools-version it can prove exists. It first
# looks for an installed copy under ~/.cache/solana; failing that it resolves
# https://github.com/anza-xyz/platform-tools/releases/latest and, when the
# requested version is newer than whatever that redirect names, logs at a level
# nobody sees and silently falls back to its own built-in default, v1.51, whose
# Cargo is 1.84. The solana 3.0 program tree pulls edition2024 crates, which
# need Cargo >= 1.85, so the fallback does not fail on the flag: it fails much
# later with "feature `edition2024` is required", pointing at a workspace member
# that has nothing to do with the programs.
#
# anza published v1.51.1 after v1.54, so that redirect now names v1.51.1 and
# every --tools-version v1.54 build takes the fallback. Installing the requested
# version ourselves removes the network lookup from the decision: the version is
# present, so it is accepted, and the same tools are used on every machine.
set -euo pipefail

readonly VERSION="${1:-${SBF_TOOLS_VERSION:-v1.54}}"
readonly ATTEMPTS=3
readonly CONNECT_TIMEOUT=15
readonly FETCH_TIMEOUT=900

readonly CACHE_DIR="${HOME}/.cache/solana"
readonly DEST="${CACHE_DIR}/${VERSION}/platform-tools"

# cargo-build-sbf reads the tools straight out of this directory, so a complete
# tree here is all it needs; it makes the rustup link itself.
if [[ -x "${DEST}/rust/bin/cargo" && -x "${DEST}/rust/bin/rustc" ]]; then
  echo "platform-tools ${VERSION} already installed at ${DEST}"
  exit 0
fi

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=osx ;;
  *) echo "unsupported OS $(uname -s) for platform-tools" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch=x86_64 ;;
  arm64 | aarch64) arch=aarch64 ;;
  *) echo "unsupported architecture $(uname -m) for platform-tools" >&2; exit 1 ;;
esac

readonly URL="https://github.com/anza-xyz/platform-tools/releases/download/${VERSION}/platform-tools-${os}-${arch}.tar.bz2"

mkdir -p "${CACHE_DIR}"

# Stage inside the cache directory so the move into place is a rename on the
# same filesystem, and name the staging directory so it holds no
# `platform-tools` child: cargo-build-sbf reads every directory here as an
# installed version, and a half-extracted one would be taken at face value.
staging="$(mktemp -d "${CACHE_DIR}/.install-sbf-tools-XXXXXX")"
trap 'rm -rf "${staging}"' EXIT

echo "Installing platform-tools ${VERSION} from ${URL}"
for ((attempt = 1; attempt <= ATTEMPTS; attempt++)); do
  if curl -sSfL \
    --connect-timeout "${CONNECT_TIMEOUT}" \
    --max-time "${FETCH_TIMEOUT}" \
    "${URL}" \
    -o "${staging}/platform-tools.tar.bz2"; then
    break
  fi

  if ((attempt == ATTEMPTS)); then
    echo "could not download platform-tools ${VERSION} after ${ATTEMPTS} attempts" >&2
    exit 1
  fi
  echo "download failed, retrying in $((attempt * 5))s" >&2
  sleep $((attempt * 5))
done

mkdir -p "${staging}/tree"
tar -C "${staging}/tree" -jxf "${staging}/platform-tools.tar.bz2"

# Only a tree with the toolchain in it is worth publishing under the version
# name, since that name is what marks the version installed.
if [[ ! -x "${staging}/tree/rust/bin/cargo" ]]; then
  echo "platform-tools ${VERSION} unpacked without rust/bin/cargo; refusing to install it" >&2
  exit 1
fi

mkdir -p "${CACHE_DIR}/${VERSION}"
rm -rf "${DEST}"
mv "${staging}/tree" "${DEST}"

echo "platform-tools ${VERSION} installed at ${DEST}"
"${DEST}/rust/bin/cargo" --version
