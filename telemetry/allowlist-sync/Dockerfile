# The allowlist sync's own image, built and pushed as BUILD.md beside this file says. Build it
# from the repository root, which holds go.mod:
#
#   docker buildx build -f telemetry/allowlist-sync/Dockerfile .
#
# Both bases are pinned by digest, so a build of one commit uses the same toolchain and base.

FROM golang:1.26.10-bookworm@sha256:5de7c99eb78ca5e515a4c340235d0955ec5964a5071c492c15503d7c267da473 AS builder

WORKDIR /workspace
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .

ARG BUILD_VERSION=undefined
ARG BUILD_COMMIT=undefined
ARG BUILD_DATE=undefined
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build \
    -ldflags "-X main.version=${BUILD_VERSION} -X main.commit=${BUILD_COMMIT} -X main.date=${BUILD_DATE}" \
    -o /out/doublezero-telemetry-allowlist-sync ./telemetry/allowlist-sync/cmd/allowlist-sync

# Ubuntu, not a distroless base: infra's Deployment starts the sync through /bin/bash -c, which
# loads its secret files into the environment, the only place the sync reads them from.
FROM ubuntu:24.04@sha256:fe81560990fae86dc4743202b2cb4d3df98d0bcf949173cecef15ce1127ef852

RUN apt-get update -qq && \
    apt-get install --no-install-recommends -y ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/doublezero-telemetry-allowlist-sync /usr/local/bin/

# The Deployment runs it as 65532 with a read-only root filesystem.
USER 65532
CMD ["doublezero-telemetry-allowlist-sync"]
