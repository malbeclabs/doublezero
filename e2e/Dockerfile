ARG PLATFORM_ARCH="amd64"

# we need to pull in the correct build of solana tools based on amd64 vs. arm64
FROM ghcr.io/malbeclabs/doublezero-base:latest AS builder

ENV CARGO_HOME=/cargo
ENV CARGO_TARGET_DIR=/target
ENV CARGO_INCREMENTAL=0

WORKDIR /doublezero
COPY . .
RUN mkdir -p bin/

# Build all rust components except the Solana program
RUN cargo build --workspace --release --exclude doublezero-sla-program && \
    cp /target/release/doublezero ./bin/ && \
    cp /target/release/doublezero-activator ./bin/ && \
    cp /target/release/doublezero-admin ./bin/

# Build the Solana program with build-sbf (rust)
RUN cd smartcontract/programs/dz-sla-program && \
    CARGO_TARGET_DIR=/target-sbf cargo build-sbf && \
    cp /target-sbf/deploy/doublezero_sla_program.so /doublezero/bin/doublezero_sla_program.so

# Build client/doublezerod (golang)
RUN make -C ./client/doublezerod build && \
    cp client/doublezerod/bin/doublezerod ./bin/

# Build the controller (golang)
RUN make -C ./controlplane/controller build && \
    cp controlplane/controller/bin/controller ./bin/doublezero-controller

# Copy test dependencies and build tests
FROM builder AS tests
COPY e2e e2e
COPY ./e2e/start_e2e.sh ./bin/start_e2e.sh
COPY ./e2e/keypair.json ./bin/keypair.json
COPY ./e2e/doublezero_keypair.json ./bin/doublezero_keypair.json
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go test -c -o ./bin/e2e_test ./e2e/e2e_test.go

# Copy everything from the previous stages into the main stage
FROM builder
ENV DEBIAN_FRONTEND=noninteractive
WORKDIR /doublezero

COPY --from=builder /usr/local/bin /usr/local/bin
ENV PATH="/usr/local/bin:${PATH}"

COPY --from=golang:1.24-alpine /usr/local/go/ /usr/local/go/
ENV PATH="/usr/local/go/bin:${PATH}"

COPY --from=builder /doublezero/bin bin
COPY --from=tests /doublezero/bin/. ./bin/.

RUN mkdir -p /root/.config/solana
RUN mkdir -p /root/.config/doublezero
COPY --from=tests /doublezero/bin/doublezero_keypair.json /root/.config/solana/id.json
COPY --from=tests /doublezero/bin/doublezero_keypair.json /root/.config/doublezero/id.json

ENV PATH="/doublezero/bin:${PATH}"

EXPOSE 7000

CMD ["sh", "./bin/start_e2e.sh"]
