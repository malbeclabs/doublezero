FROM ubuntu:22.04 AS builder

ENV DEBIAN_FRONTEND=noninteractive

RUN apt update && apt install --no-install-recommends -y \
    ca-certificates \
    curl \
    build-essential \
    pkg-config \
    mold \
    libudev-dev llvm libclang-dev \
    protobuf-compiler libssl-dev git iproute2

# Install rust
RUN curl https://sh.rustup.rs -sSf | sh -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"

# Enable mold linker for faster builds
ENV RUSTFLAGS="-C link-arg=-fuse-ld=mold"

# Install solana cli.
# Pinned to the deploy floor: testnet runs agave 2.2.16 and mainnet 2.3.6, so
# 2.2.16 is the binding constraint a change must run on to be deployable.
# Build from the git tag (not release.anza.xyz) because Anza publishes no
# aarch64-linux release binary, so a source build is required to keep this image
# multi-arch (amd64 + arm64, the latter for local Apple Silicon e2e runs). The
# fixed tag also makes this layer cacheable, unlike an unpinned HEAD clone.
# Bump when the deploy floor moves.
RUN git clone --branch v2.2.16 --depth 1 https://github.com/anza-xyz/agave.git && \
    ./agave/scripts/cargo-install-all.sh /usr/local/ && \
    rm -rf agave

FROM ubuntu:22.04

COPY --from=builder /usr/local/bin /usr/local/bin

CMD [ "/bin/bash" ]
