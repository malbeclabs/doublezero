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
#
# rust/lib is checked alongside the binaries because an interrupted extraction
# leaves the tree in exactly that state: the binaries are there and the standard
# library is not. cargo-build-sbf takes the version directory's existence as
# proof the version is installed and would build against the wreckage, so an
# incomplete tree has to read as absent here and be replaced below.
#
# llvm/bin/clang is the same case one subtree over. cargo-build-sbf exports
# platform-tools' clang, llvm-ar, llvm-objdump and llvm-objcopy as CC, AR,
# OBJDUMP and OBJCOPY, and this tree compiles C (blake3), so an extraction that
# stopped between rust/ and llvm/ leaves every later build failing on a missing
# clang with nothing to repair it.
installed() {
  [[ -x "$1/rust/bin/cargo" && -x "$1/rust/bin/rustc" && -d "$1/rust/lib" \
    && -x "$1/llvm/bin/clang" ]]
}

if installed "${DEST}"; then
  echo "platform-tools ${VERSION} already installed at ${DEST}"
  exit 0
fi

# One cache, several writers. The e2e images mount `sbf-solana-<solana version>`
# with `sharing=shared`, so two image builds on one self-hosted runner install
# into the same ~/.cache/solana. Unserialized, run B can delete the tree run A is
# reading; worse, if B recreates the version directory between A's `rm -rf` and
# A's `mv`, the move lands inside it and the cache ends up holding
# platform-tools/tree/rust/..., which is not empty and therefore reads to
# cargo-build-sbf as an installed version.
#
# mkdir is the mutex because it is atomic on every filesystem this runs on,
# unlike `flock`, which macOS does not ship.
readonly LOCK="${CACHE_DIR}/.install-sbf-tools-${VERSION}.lock"
# Long enough that a live install cannot have its lock taken: the download alone
# is allowed ATTEMPTS * FETCH_TIMEOUT, and extraction follows. A shorter wait
# would declare a slow-but-working install stale and put a second writer beside
# it, which is what the lock exists to prevent. The lock is still only advisory
# against writers that take it, so the deadline only has to outlast this script.
readonly LOCK_TIMEOUT=$((ATTEMPTS * FETCH_TIMEOUT + 600))

mkdir -p "${CACHE_DIR}"
waited=0
until mkdir "${LOCK}" 2>/dev/null; do
  if ((waited >= LOCK_TIMEOUT)); then
    echo "lock at ${LOCK} held for ${LOCK_TIMEOUT}s; assuming it is stale" >&2
    rm -rf "${LOCK}"
    continue
  fi
  sleep 2
  waited=$((waited + 2))
done
trap 'rm -rf "${LOCK}"' EXIT

# Another writer may have finished while this one waited.
if installed "${DEST}"; then
  echo "platform-tools ${VERSION} installed at ${DEST} while waiting"
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

# Stage inside the cache directory so the move into place is a rename on the
# same filesystem, and name the staging directory so it holds no
# `platform-tools` child: cargo-build-sbf reads every directory here as an
# installed version, and a half-extracted one would be taken at face value.
staging="$(mktemp -d "${CACHE_DIR}/.install-sbf-tools-XXXXXX")"
trap 'rm -rf "${staging}" "${LOCK}"' EXIT

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

# Only a complete tree is worth publishing under the version name, since that
# name is what marks the version installed. This is the same test the cached
# tree had to pass above; a download that fails it leaves nothing behind for the
# next run to trust.
if ! installed "${staging}/tree"; then
  echo "platform-tools ${VERSION} unpacked incomplete; refusing to install it" >&2
  exit 1
fi

mkdir -p "${CACHE_DIR}/${VERSION}"
rm -rf "${DEST}"

# The lock serializes this script against itself, but cargo-build-sbf is a
# writer that never takes it: install_if_missing creates the version directory
# before it downloads. If it does that in the gap the delete above opens, a
# plain `mv` lands inside the directory it created and leaves the tree at
# platform-tools/tree/rust, which is not empty and so reads as installed. GNU
# mv -T renames onto the destination path itself and cannot nest. BSD mv has no
# -T; it also has no shared cache to protect, and after the delete above the
# destination is absent, so there the plain rename is just a rename.
if ! mv -T "${staging}/tree" "${DEST}" 2>/dev/null; then
  mv "${staging}/tree" "${DEST}"
fi

echo "platform-tools ${VERSION} installed at ${DEST}"
"${DEST}/rust/bin/cargo" --version
