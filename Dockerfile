# ----------------------------------------------------------------------------
# Builder stage for the doublezero components.
# ----------------------------------------------------------------------------
FROM ubuntu:24.04 AS builder-base

# Install build dependencies and other utilities
RUN apt update -qq && \
    apt install --no-install-recommends -y \
    ca-certificates \
    curl \
    build-essential \
    pkg-config \
    mold \
    libudev-dev llvm libclang-dev \
    protobuf-compiler libssl-dev git iproute2 iputils-ping net-tools tcpdump

# Install go
ARG GO_VERSION=1.25.5
RUN ARCH="$(uname -m)" && \
    case "$ARCH" in \
    x86_64) GOARCH=amd64 ;; \
    aarch64) GOARCH=arm64 ;; \
    *) echo "Unsupported architecture: $ARCH" && exit 1 ;; \
    esac && \
    curl -sSL "https://go.dev/dl/go${GO_VERSION}.linux-${GOARCH}.tar.gz" | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:/root/go/bin:${PATH}"


# -----------------------------------------------------------------------------
# Go builder
#
# We build the go components in a single stage, to take advantage of caching
# across components.
# -----------------------------------------------------------------------------
FROM builder-base AS builder-go

WORKDIR /doublezero
COPY . .
RUN mkdir -p bin/

# Set build arguments
ARG BUILD_VERSION=undefined
ARG BUILD_COMMIT=undefined
ARG BUILD_DATE=undefined

RUN if [ "${BUILD_VERSION}" = "undefined" ] || [ "${BUILD_COMMIT}" = "undefined" ] || [ "${BUILD_DATE}" = "undefined" ]; then \
    echo "Build arguments must be defined" && \
    exit 1; \
    fi

ENV CGO_ENABLED=0
ENV GO_LDFLAGS="-X main.version=${BUILD_VERSION} -X main.commit=${BUILD_COMMIT} -X main.date=${BUILD_DATE}"

# Set up a binaries directory
ENV BIN_DIR=/doublezero/bin
RUN mkdir -p ${BIN_DIR}

# Build api (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-api api/cmd/server/main.go

# Build the funder (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-funder controlplane/funder/cmd/funder/main.go

# Build the monitor (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-monitor controlplane/monitor/cmd/monitor/main.go

# Build the telemetry data API (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-telemetry-data-api controlplane/telemetry/cmd/data-api/main.go

# Build the telemetry data CLI (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-telemetry-data-cli controlplane/telemetry/cmd/data-cli/main.go

# Build the telemetry flow enricher (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-telemetry-flow-enricher telemetry/flow-enricher/cmd/flow-enricher/main.go

# Build the telemetry flow ingest server (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-telemetry-flow-ingest telemetry/flow-ingest/cmd/server/main.go

# Build telemetry state-ingest server (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-telemetry-state-ingest telemetry/state-ingest/cmd/server/main.go

# Build telemetry gnmi-writer (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-gnmi-writer telemetry/gnmi-writer/cmd/gnmi-writer/main.go

# Build device-health-oracle (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-device-health-oracle controlplane/device-health-oracle/cmd/device-health-oracle/main.go

# Force COPY in later stages to always copy the binaries, even if they appear to be the same.
ARG CACHE_BUSTER=1
RUN echo "$CACHE_BUSTER" > ${BIN_DIR}/.cache-buster && \
    find ${BIN_DIR} -type f -exec touch {} +


# ----------------------------------------------------------------------------
# Main stage with only the binaries.
# ----------------------------------------------------------------------------
FROM ubuntu:24.04

# Install build dependencies and other utilities
RUN apt update -qq && \
    apt install --no-install-recommends -y \
    ca-certificates \
    curl \
    build-essential \
    pkg-config \
    iproute2 iputils-ping net-tools tcpdump

ENV PATH="/doublezero/bin:${PATH}"

# Copy binaries from the builder stage.
COPY --from=builder-go /doublezero/bin/. /doublezero/bin/.

CMD ["/bin/bash"]
