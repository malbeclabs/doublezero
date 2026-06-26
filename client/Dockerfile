# Thin DoubleZero client image.
#
# Installs the `doublezero` package (CLI + `doublezerod` daemon) from the official
# Cloudsmith apt repo on top of a minimal Ubuntu base. The entrypoint starts the
# daemon and passes any arguments through to the `doublezero` CLI, e.g.:
#
#   docker run --rm --network host \
#     --cap-add NET_ADMIN --cap-add NET_RAW --device /dev/net/tun \
#     ghcr.io/malbeclabs/doublezero status
#
# See client/docker-README.md for the full runtime contract.
#
# The doublezero deb is published for linux/amd64 only, so the platform defaults to
# amd64 (via an ARG, to avoid the const-platform build warning) to keep builds
# reproducible on arm64 hosts such as Apple Silicon. Override with
# --build-arg BASE_PLATFORM=... if needed. Override the release-train repo with
# --build-arg DZ_CLOUDSMITH_REPO=<repo>, where <repo> is one of:
# doublezero (mainnet-beta), doublezero-testnet, doublezero-devnet.
ARG BASE_PLATFORM=linux/amd64
FROM --platform=${BASE_PLATFORM} ubuntu:24.04

# Which Cloudsmith apt repo to install the client from. Selects the environment's
# release train:
#   doublezero          -> mainnet-beta (public/promoted repo)
#   doublezero-testnet  -> testnet
#   doublezero-devnet   -> devnet (nightly; see DZ_DEP_REPO for its solana dep)
ARG DZ_CLOUDSMITH_REPO=doublezero
# Optional second Cloudsmith repo added only to satisfy dependencies that the
# primary repo lacks (the devnet repo has no doublezero-solana). Empty for the
# self-contained testnet/mainnet-beta repos.
ARG DZ_DEP_REPO=
# Default environment baked into the image so `docker run` with no -e DZ_ENV
# still selects the right env. The entrypoint honors DZ_ENV.
ARG DZ_DEFAULT_ENV=mainnet-beta
# Empty installs the repo's latest. CI pins the exact resolved version for
# reproducible images (required for devnet, whose nightly version is ambiguous
# once the dep repo is also present).
ARG DZ_VERSION=
# Image provenance, supplied by CI; sensible defaults for local builds.
ARG BUILD_VERSION=dev
ARG BUILD_COMMIT=unknown

LABEL org.opencontainers.image.source="https://github.com/malbeclabs/doublezero" \
      org.opencontainers.image.title="doublezero" \
      org.opencontainers.image.description="Thin DoubleZero client (doublezero CLI + doublezerod daemon)" \
      org.opencontainers.image.version="${BUILD_VERSION}" \
      org.opencontainers.image.revision="${BUILD_COMMIT}"

ENV DEBIAN_FRONTEND=noninteractive
# Baked default environment for the entrypoint; overridable at runtime with -e DZ_ENV.
ENV DZ_ENV=${DZ_DEFAULT_ENV}

# Add the primary Cloudsmith apt repo (and an optional dependency repo) via the
# official scripted installer, install the client, then drop the now-unneeded
# curl and apt lists to keep the image thin. ca-certificates is installed
# explicitly so it survives the curl auto-remove.
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl; \
    curl -1sLf "https://dl.cloudsmith.io/public/malbeclabs/${DZ_CLOUDSMITH_REPO}/setup.deb.sh" | bash; \
    if [ -n "${DZ_DEP_REPO}" ]; then \
      curl -1sLf "https://dl.cloudsmith.io/public/malbeclabs/${DZ_DEP_REPO}/setup.deb.sh" | bash; \
    fi; \
    apt-get update; \
    apt-get install -y --no-install-recommends "doublezero${DZ_VERSION:+=$DZ_VERSION}"; \
    apt-get purge -y --auto-remove curl; \
    rm -rf /var/lib/apt/lists/*

COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
