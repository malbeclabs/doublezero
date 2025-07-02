FROM ubuntu:24.04 AS base
# Mirrors: https://launchpad.net/ubuntu/+archivemirrors
ARG ARM64_UBUNTU_MIRROR=https://mirror.mci-1.serverforge.org/ubuntu-ports/
ARG AMD64_UBUNTU_MIRROR=http://mirror.math.princeton.edu/pub/ubuntu/
RUN ARCH=$(dpkg --print-architecture) && \
    CODENAME="$(. /etc/os-release && echo "$VERSION_CODENAME")" && \
    UBUNTU_MIRROR=$( \
    case "$ARCH" in \
    amd64) echo "${AMD64_UBUNTU_MIRROR}" ;; \
    arm64) echo "${ARM64_UBUNTU_MIRROR}" ;; \
    *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;; \
    esac ) && \
    echo "Using mirror: $UBUNTU_MIRROR for $ARCH ($CODENAME)" && \
    echo "deb ${UBUNTU_MIRROR} ${CODENAME} main restricted universe multiverse\n\
    deb ${UBUNTU_MIRROR} ${CODENAME}-updates main restricted universe multiverse\n\
    deb ${UBUNTU_MIRROR} ${CODENAME}-security main restricted universe multiverse" \
    > /etc/apt/sources.list && \
    apt-get update -qq && \
    apt-get install -y --no-install-recommends curl ca-certificates

FROM golang:1.24.3-alpine AS builder
WORKDIR /work
COPY . .
WORKDIR /work/client/doublezerod/internal/runtime
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go test -c -o /bin/runtime.test -tags e2e .

FROM base
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates iproute2
WORKDIR /work
COPY --from=builder /bin/runtime.test /bin/
COPY --from=builder /work/client/doublezerod/internal/runtime .
ENTRYPOINT ["/bin/runtime.test"]
CMD ["-test.v"]
