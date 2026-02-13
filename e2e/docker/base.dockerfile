# ----------------------------------------------------------------------------
# Solana stage with a platform-specific image.
# ----------------------------------------------------------------------------
FROM ubuntu:24.04 AS solana

RUN apt update -qq && \
    apt install --no-install-recommends -y ca-certificates curl bzip2

# Install agave/solana tools
ARG SOLANA_VERSION=2.3.13
RUN bash -c 'set -euo pipefail; curl -fsSL https://release.anza.xyz/v${SOLANA_VERSION}/install | sh'
RUN mkdir -p /opt/solana/bin && \
    mv /root/.local/share/solana/install/active_release/bin/* /opt/solana/bin/
ENV PATH="/opt/solana/bin:${PATH}"

# CACHE_BUSTER should be passed as a build arg with a changing value (e.g. timestamp)
# to force Docker to re-run build commands, allowing cargo/go to do proper incremental
# compilation. Without this, switching branches can result in stale cached artifacts.
# It also forces COPY in later stages to always copy the binaries.
ARG CACHE_BUSTER=1
RUN echo "$CACHE_BUSTER" > /opt/solana/bin/.cache-buster && \
    find /opt/solana/bin -type f -exec touch {} +

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

# Install rust
RUN curl https://sh.rustup.rs -sSf | sh -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"

# Copy all the solana binaries
COPY --from=solana /opt/solana/bin/. /usr/local/bin/.


# -----------------------------------------------------------------------------
# Rust workspace builder
#
# We can build the whole rust workspace in a single stage, to take advantage of
# caching and cargo's own parallelization.
# -----------------------------------------------------------------------------
FROM builder-base AS builder-rust

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

# Build all rust components except the Solana program
RUN --mount=type=cache,target=/cargo \
    --mount=type=cache,target=/target \
    RUSTFLAGS="-C link-arg=-fuse-ld=mold" cargo build --workspace --release --exclude doublezero-serviceability --exclude doublezero-telemetry && \
    cp /target/release/doublezero ${BIN_DIR}/ && \
    cp /target/release/doublezero-activator ${BIN_DIR}/ && \
    cp /target/release/doublezero-admin ${BIN_DIR}/ && \
    cp /target/release/fork-accounts ${BIN_DIR}/

# Force COPY in later stages to always copy the binaries, even if they appear to be the same.
ARG CACHE_BUSTER=1
RUN echo "$CACHE_BUSTER" > ${BIN_DIR}/.cache-buster && \
    find ${BIN_DIR} -type f -exec touch {} +

# -----------------------------------------------------------------------------
# Solana program builder (rust)
#
# This builds for a different target than the rest of the rust workspace, so
# we build it in a separate stage so it's parallelized and cached separately.
# -----------------------------------------------------------------------------
FROM builder-base AS builder-rust-sbf

# Solana version for cache isolation. Cache mounts are keyed by SOLANA_VERSION so that
# when the Solana SDK version changes, we get a fresh cache instead of using potentially
# corrupted or incompatible cached platform-tools. This must match SOLANA_VERSION in the
# solana stage above.
ARG SOLANA_VERSION=2.3.13

# Set cargo environment variables for build caching
ENV CARGO_HOME=/cargo-sbf
ENV CARGO_TARGET_DIR=/target-sbf
ENV CARGO_INCREMENTAL=0

WORKDIR /doublezero
COPY . .

# Pre-fetch and cache rust dependencies
RUN --mount=type=cache,id=sbf-cargo-${SOLANA_VERSION},target=/cargo-sbf \
    --mount=type=cache,id=sbf-target-${SOLANA_VERSION},target=/target-sbf \
    --mount=type=cache,id=sbf-solana-${SOLANA_VERSION},target=/root/.cache/solana \
    cd smartcontract/programs/doublezero-serviceability && \
    cargo fetch

RUN --mount=type=cache,id=sbf-cargo-${SOLANA_VERSION},target=/cargo-sbf \
    --mount=type=cache,id=sbf-target-${SOLANA_VERSION},target=/target-sbf \
    --mount=type=cache,id=sbf-solana-${SOLANA_VERSION},target=/root/.cache/solana \
    cd smartcontract/programs/doublezero-telemetry && \
    cargo fetch

# Set up a binaries directory
ENV BIN_DIR=/doublezero/bin
RUN mkdir -p ${BIN_DIR}

# Build the Solana programs with build-sbf (rust)
# Note that we don't use mold here.
RUN --mount=type=cache,id=sbf-cargo-${SOLANA_VERSION},target=/cargo-sbf \
    --mount=type=cache,id=sbf-target-${SOLANA_VERSION},target=/target-sbf \
    --mount=type=cache,id=sbf-solana-${SOLANA_VERSION},target=/root/.cache/solana \
    cd smartcontract/programs/doublezero-serviceability && \
    cargo build-sbf && \
    cp /target-sbf/deploy/doublezero_serviceability.so ${BIN_DIR}/doublezero_serviceability.so

RUN --mount=type=cache,id=sbf-cargo-${SOLANA_VERSION},target=/cargo-sbf \
    --mount=type=cache,id=sbf-target-${SOLANA_VERSION},target=/target-sbf \
    --mount=type=cache,id=sbf-solana-${SOLANA_VERSION},target=/root/.cache/solana \
    cd smartcontract/programs/doublezero-telemetry && \
    cargo build-sbf --features localnet && \
    cp /target-sbf/deploy/doublezero_telemetry.so ${BIN_DIR}/doublezero_telemetry.so

# Force COPY in later stages to always copy the programs, even if they appear to be the same.
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

# Set up a binaries directory
ENV BIN_DIR=/doublezero/bin
RUN mkdir -p ${BIN_DIR}

# Build client/doublezerod (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make -C ./client/doublezerod build && \
    cp client/doublezerod/bin/doublezerod ${BIN_DIR}/

# Build the controller (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make -C ./controlplane/controller build && \
    cp controlplane/controller/bin/controller ${BIN_DIR}/doublezero-controller

# Build the funder (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -o ${BIN_DIR}/doublezero-funder controlplane/funder/cmd/funder/main.go

# Build the device-health-oracle (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -o ${BIN_DIR}/device-health-oracle controlplane/device-health-oracle/cmd/device-health-oracle/main.go

# Build the QA agent (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -o ${BIN_DIR}/doublezero-qaagent ./e2e/cmd/qaagent/

# Force COPY in later stages to always copy the binaries, even if they appear to be the same.
ARG CACHE_BUSTER=1
RUN echo "$CACHE_BUSTER" > ${BIN_DIR}/.cache-buster && \
    find ${BIN_DIR} -type f -exec touch {} +


# ----------------------------------------------------------------------------
# Main stage with only the binaries.
# ----------------------------------------------------------------------------
FROM ubuntu:24.04

# Copy binaries from the builder stage.
COPY --from=solana /opt/solana/bin/solana-test-validator /usr/local/bin/.
COPY --from=solana /opt/solana/bin/solana /usr/local/bin/.
COPY --from=solana /opt/solana/bin/solana-keygen /usr/local/bin/.
COPY --from=builder-rust /doublezero/bin/. /doublezero/bin/.
COPY --from=builder-rust-sbf /doublezero/bin/. /doublezero/bin/.
COPY --from=builder-go /doublezero/bin/. /doublezero/bin/.

CMD ["/bin/bash"]
