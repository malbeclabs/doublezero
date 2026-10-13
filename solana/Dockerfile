ARG RUST_VERSION=1.91
ARG SOLANA_VERSION=v3.0.12

FROM rust:${RUST_VERSION}-slim AS builder

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    curl \
    make

ARG SOLANA_VERSION
RUN sh -c "$(curl -sSfL https://release.anza.xyz/${SOLANA_VERSION}/install)"
ENV PATH="/root/.local/share/solana/install/active_release/bin:${PATH}"
RUN solana --version

WORKDIR /build

COPY rust-toolchain.toml Cargo.toml Cargo.lock Makefile ./
COPY programs ./programs
COPY crates ./crates
COPY mock ./mock

RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/usr/local/cargo/git \
    cargo fetch --locked

ARG NETWORK
# Cache the SBF platform-tools toolchain (~470MB, fetched by cargo build-sbf into
# ~/.cache/solana) so rebuilds reuse it instead of re-downloading it every time.
# `target` is a cache mount shared across networks. It is locked so concurrent
# builds cannot interleave, and each program is copied by name rather than
# globbing a stale .so from another network's build.
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/usr/local/cargo/git \
    --mount=type=cache,target=/build/target,sharing=locked \
    --mount=type=cache,target=/root/.cache/solana \
    set -e; \
    mkdir "artifacts-${NETWORK}"; \
    NETWORK=${NETWORK} make build-sbf; \
    cp target/deploy/doublezero_builder_stake.so \
        target/deploy/doublezero_passport.so \
        target/deploy/doublezero_revenue_distribution.so \
        "artifacts-${NETWORK}/"

FROM scratch AS artifacts
ARG NETWORK
COPY --from=builder /build/artifacts-${NETWORK}/*.so /
