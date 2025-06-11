# ----------------------------------------------------------------------------
# Solana stage with a platform-specific image.
# ----------------------------------------------------------------------------
FROM ghcr.io/malbeclabs/solana:latest AS solana


# ----------------------------------------------------------------------------
# Builder stage for the doublezero components.
# ----------------------------------------------------------------------------
FROM golang:1.24-bookworm AS builder-base

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

# Install rust
RUN curl https://sh.rustup.rs -sSf | sh -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"

# Copy all the solana binaries
COPY --from=solana /usr/local/bin/. /usr/local/bin/.


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
    RUSTFLAGS="-C link-arg=-fuse-ld=mold" cargo build --workspace --release --exclude doublezero-sla-program && \
    cp /target/release/doublezero ${BIN_DIR}/ && \
    cp /target/release/doublezero-activator ${BIN_DIR}/ && \
    cp /target/release/doublezero-admin ${BIN_DIR}/


# -----------------------------------------------------------------------------
# Solana program builder (rust)
#
# This builds for a different target than the rest of the rust workspace, so
# we build it in a separate stage so it's parallelized and cached separately.
# -----------------------------------------------------------------------------
FROM builder-base AS builder-rust-sbf

# Set cargo environment variables for build caching
ENV CARGO_HOME=/cargo-sbf
ENV CARGO_TARGET_DIR=/target-sbf
ENV CARGO_INCREMENTAL=0

WORKDIR /doublezero
COPY . .

# Pre-fetch and cache rust dependencies
RUN --mount=type=cache,target=/cargo-sbf \
    --mount=type=cache,target=/target-sbf \
    cd smartcontract/programs/dz-sla-program && \
    cargo fetch

# Set up a binaries directory
ENV BIN_DIR=/doublezero/bin
RUN mkdir -p ${BIN_DIR}

# Build the Solana program with build-sbf (rust)
# Note that we don't use mold here.
RUN --mount=type=cache,target=/cargo-sbf \
    --mount=type=cache,target=/target-sbf \
    --mount=type=cache,target=/root/.cache/solana \
    cd smartcontract/programs/dz-sla-program && \
    cargo build-sbf && \
    cp /target-sbf/deploy/doublezero_sla_program.so ${BIN_DIR}/doublezero_sla_program.so


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

# (Removed commented-out code for building the agent)


# ----------------------------------------------------------------------------
# Main stage with only the binaries.
# ----------------------------------------------------------------------------
FROM ubuntu:24.04

# Copy binaries from the builder stage.
COPY --from=solana /usr/local/bin/solana-test-validator /usr/local/bin/.
COPY --from=solana /usr/local/bin/solana /usr/local/bin/.
COPY --from=solana /usr/local/bin/solana-keygen /usr/local/bin/.
COPY --from=builder-rust /doublezero/bin/. /doublezero/bin/.
COPY --from=builder-rust-sbf /doublezero/bin/. /doublezero/bin/.
COPY --from=builder-go /doublezero/bin/. /doublezero/bin/.

CMD ["/bin/bash"]
