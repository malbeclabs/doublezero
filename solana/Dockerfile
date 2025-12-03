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

RUN cargo fetch --locked

ARG NETWORK
RUN set -e; \
    mkdir "artifacts-${NETWORK}"; \
    NETWORK=${NETWORK} make build-sbf

FROM scratch AS artifacts
COPY --from=builder /build/target/deploy/*.so /
