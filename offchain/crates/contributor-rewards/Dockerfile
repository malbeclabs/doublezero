FROM rust:1.97.1-bookworm@sha256:0e2bcaef56d041a486784e54104a81aebe0da44bd03019bd70bc0401e42e4a97 AS builder

RUN apt-get update -qq && \
    apt-get install --no-install-recommends -y \
    cmake \
    pkg-config \
    libssl-dev \
    libudev-dev \
    llvm \
    libclang-dev \
    protobuf-compiler && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /doublezero
COPY . .

RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/doublezero/target \
    cargo build --release --locked -p doublezero-contributor-rewards --bin doublezero-contributor-rewards && \
    cp target/release/doublezero-contributor-rewards /usr/local/bin/

FROM debian:bookworm-slim@sha256:7ff6ba31527b9e336162fa061746c366c4b2032f4d2cff2078bea43f78efe3f8

RUN apt-get update -qq && \
    apt-get install --no-install-recommends -y \
    ca-certificates \
    libssl3 \
    libudev1 && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /usr/local/bin/doublezero-contributor-rewards /usr/local/bin/

USER 65532:65532

ENTRYPOINT ["doublezero-contributor-rewards"]
