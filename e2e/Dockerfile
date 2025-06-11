FROM ghcr.io/malbeclabs/solana:latest AS solana

# -----------------------------------------------------------------------------
# Base builder stage includes go, rust, and other system dependencies that are
# shared across all builder stages.
# -----------------------------------------------------------------------------
FROM golang:1.24-bookworm AS base

# Install system dependencies and utilities
RUN apt update && apt install --no-install-recommends -y \
    ca-certificates \
    curl \
    build-essential \
    pkg-config \
    mold \
    libudev-dev llvm libclang-dev \
    protobuf-compiler libssl-dev git iproute2 iputils-ping iproute2 net-tools tcpdump

# Install rust
RUN curl https://sh.rustup.rs -sSf | sh -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"

# Copy in the solana tools from the solana builder stage.
COPY --from=solana /usr/local/bin /usr/local/bin

# -----------------------------------------------------------------------------
# Rust workspace builder
#
# We can build the whole rust workspace in a single stage, to take advantage of
# caching and cargo's own parallelization.
# -----------------------------------------------------------------------------
FROM base AS builder-rust

ENV CARGO_HOME=/cargo
ENV CARGO_TARGET_DIR=/target
ENV CARGO_INCREMENTAL=0

WORKDIR /doublezero
COPY . .
RUN mkdir -p bin/

# Pre-fetch and cache rust dependencies
RUN --mount=type=cache,target=/cargo \
    --mount=type=cache,target=/target \
    cargo fetch

# Build all rust components except the Solana program
RUN --mount=type=cache,target=/cargo \
    --mount=type=cache,target=/target \
    RUSTFLAGS="-C link-arg=-fuse-ld=mold" cargo build --workspace --release --exclude doublezero-serviceability && \
    cp /target/release/doublezero ./bin/ && \
    cp /target/release/doublezero-activator ./bin/ && \
    cp /target/release/doublezero-admin ./bin/

# -----------------------------------------------------------------------------
# Solana program builder (rust)
#
# This builds for a different target than the rest of the rust workspace, so
# we build it in a separate stage so it's parallelized and cached separately.
# -----------------------------------------------------------------------------
FROM base AS builder-sbf

ENV CARGO_HOME=/cargo-sbf
ENV CARGO_TARGET_DIR=/target-sbf
ENV CARGO_INCREMENTAL=0

WORKDIR /doublezero
COPY . .
RUN mkdir -p bin/

# Pre-fetch and cache rust dependencies
RUN --mount=type=cache,target=/cargo-sbf \
    --mount=type=cache,target=/target-sbf \
    cd smartcontract/programs/doublezero-serviceability && \
    cargo fetch

# Build the Solana program with build-sbf (rust)
# Note that we don't use mold here.
RUN --mount=type=cache,target=/cargo-sbf \
    --mount=type=cache,target=/target-sbf \
    --mount=type=cache,target=/root/.cache \
    cd smartcontract/programs/doublezero-serviceability && \
    cargo build-sbf && \
    cp /target-sbf/deploy/doublezero_serviceability.so /doublezero/bin/doublezero_serviceability.so

# -----------------------------------------------------------------------------
# Go builder
#
# We build the go components in a single stage, to take advantage of caching
# across components.
# -----------------------------------------------------------------------------
FROM base AS builder-go

WORKDIR /doublezero
COPY . .
RUN mkdir -p bin/

# Build client/doublezerod (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make -C ./client/doublezerod build && \
    cp client/doublezerod/bin/doublezerod ./bin/

# Build the controller (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make -C ./controlplane/controller build && \
    cp controlplane/controller/bin/controller ./bin/doublezero-controller

# -----------------------------------------------------------------------------
# Test builder stage builds the e2e tests binary.
# -----------------------------------------------------------------------------
FROM builder-go AS tests
COPY e2e e2e
RUN cp ./e2e/start_e2e.sh ./bin/start_e2e.sh && \
    cp ./e2e/keypair.json ./bin/keypair.json && \
    cp ./e2e/doublezero_keypair.json ./bin/doublezero_keypair.json
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go test -c -o ./bin/e2e_test ./e2e/e2e_test.go

# -----------------------------------------------------------------------------
# Main stage
#
# Copy everything we need from the previous stages into the main stage.
# -----------------------------------------------------------------------------
FROM base
ENV DEBIAN_FRONTEND=noninteractive
WORKDIR /doublezero

# Copy our binaries from the builder and test stages.
COPY --from=builder-rust /doublezero/bin/. bin/.
COPY --from=builder-sbf /doublezero/bin/. bin/.
COPY --from=builder-go /doublezero/bin/. bin/.
COPY --from=tests /doublezero/bin/. ./bin/.

# Set up the config directories and keypairs for testing.
RUN mkdir -p /root/.config/solana
RUN mkdir -p /root/.config/doublezero
COPY --from=tests /doublezero/bin/doublezero_keypair.json /root/.config/solana/id.json
COPY --from=tests /doublezero/bin/doublezero_keypair.json /root/.config/doublezero/id.json

ENV PATH="/doublezero/bin:${PATH}"

EXPOSE 7000

CMD ["sh", "./bin/start_e2e.sh"]
