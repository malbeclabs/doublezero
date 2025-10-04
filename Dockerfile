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
ARG GO_VERSION=1.24.3
RUN ARCH="$(uname -m)" && \
    case "$ARCH" in \
    x86_64) GOARCH=amd64 ;; \
    aarch64) GOARCH=arm64 ;; \
    *) echo "Unsupported architecture: $ARCH" && exit 1 ;; \
    esac && \
    curl -sSL "https://go.dev/dl/go${GO_VERSION}.linux-${GOARCH}.tar.gz" | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:/root/go/bin:${PATH}"

# Install rust
RUN curl https://sh.rustup.rs -sSf | sh -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"


# -----------------------------------------------------------------------------
# Rust workspace builder
#
# We can build the whole rust workspace in a single stage, to take advantage of
# caching and cargo's own parallelization.
# -----------------------------------------------------------------------------
FROM builder-base AS builder-rust

# Set build arguments
ARG DZ_ENV=localnet
ARG BUILD_VERSION=undefined
ARG BUILD_COMMIT=undefined
ARG BUILD_DATE=undefined

ENV BUILD_VERSION=${BUILD_VERSION}
ENV BUILD_COMMIT=${BUILD_COMMIT}
ENV BUILD_DATE=${BUILD_DATE}

RUN if [ "${DZ_ENV}" = "undefined" ]; then \
    echo "DZ_ENV must be defined" && \
    exit 1; \
    fi

RUN if [ "${BUILD_VERSION}" = "undefined" ] || [ "${BUILD_COMMIT}" = "undefined" ] || [ "${BUILD_DATE}" = "undefined" ]; then \
    echo "Build arguments must be defined" && \
    exit 1; \
    fi

# Set cargo environment variables for build caching
ENV CARGO_HOME=/cargo
ENV CARGO_TARGET_DIR=/target
ENV CARGO_INCREMENTAL=0

WORKDIR /doublezero
COPY . .

# Pre-fetch and cache rust dependencies
RUN --mount=type=cache,target=/cargo \
    --mount=type=cache,target=/target \
    cargo fetch

# Set up a binaries directory
ENV BIN_DIR=/doublezero/bin
RUN mkdir -p ${BIN_DIR}

# Build all rust components.
RUN --mount=type=cache,target=/cargo \
    --mount=type=cache,target=/target \
    RUSTFLAGS="-C link-arg=-fuse-ld=mold" cargo build --workspace --release && \
    cp /target/release/doublezero ${BIN_DIR}/ && \
    cp /target/release/doublezero-activator ${BIN_DIR}/ && \
    cp /target/release/doublezero-admin ${BIN_DIR}/

# Force COPY in later stages to always copy the binaries, even if they appear to be the same.
ARG CACHE_BUSTER=1
RUN echo "$CACHE_BUSTER" > ${BIN_DIR}/.cache-buster && \
    find ${BIN_DIR} -type f -exec touch {} +


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

# Build config agent (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-config-agent controlplane/agent/cmd/agent/main.go

# Build telemetry agent (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-telemetry-agent controlplane/telemetry/cmd/telemetry/main.go

# Build client/doublezerod (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezerod client/doublezerod/cmd/doublezerod/main.go

# Build the controller (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-controller controlplane/controller/cmd/controller/main.go

# Build the funder (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-funder controlplane/funder/cmd/funder/main.go

# Build the monitor (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-monitor controlplane/monitor/cmd/monitor/main.go

# Build the qa agent (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-qa-agent e2e/cmd/qaagent/main.go

# Build the internet latency collector (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-internet-latency-collector controlplane/internet-latency-collector/cmd/collector/main.go

# Build the telemetry data API (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-telemetry-data-api controlplane/telemetry/cmd/data-api/main.go

# Build the telemetry data CLI (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-telemetry-data-cli controlplane/telemetry/cmd/data-cli/main.go

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
COPY --from=builder-rust /doublezero/bin/. /doublezero/bin/.
COPY --from=builder-go /doublezero/bin/. /doublezero/bin/.

CMD ["/bin/bash"]
