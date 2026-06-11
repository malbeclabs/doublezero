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
# --build-arg BASE_PLATFORM=... if needed.
ARG BASE_PLATFORM=linux/amd64
FROM --platform=${BASE_PLATFORM} ubuntu:24.04

# Empty DZ_VERSION installs the latest published client. Release CI pins the exact
# released version (e.g. DZ_VERSION=1.2.3) for reproducible images.
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

# Add the Cloudsmith apt repo via the official scripted installer, install the
# client, then drop the now-unneeded curl and apt lists to keep the image thin.
# ca-certificates is installed explicitly so it survives the curl auto-remove.
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl; \
    curl -1sLf 'https://dl.cloudsmith.io/public/malbeclabs/doublezero/setup.deb.sh' | bash; \
    apt-get install -y --no-install-recommends "doublezero${DZ_VERSION:+=$DZ_VERSION}"; \
    apt-get purge -y --auto-remove curl; \
    rm -rf /var/lib/apt/lists/*

COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
