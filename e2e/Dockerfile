ARG PLATFORM_ARCH="amd64"

# we need to pull in the correct build of solana tools based on amd64 vs. arm64
FROM ghcr.io/malbeclabs/doublezero-base:latest AS builder

ENV CARGO_HOME=/cargo
ENV CARGO_TARGET_DIR=/target
ENV CARGO_INCREMENTAL=0

WORKDIR /doublezero
COPY go.mod go.sum ./
RUN mkdir -p bin/

# Below creates a build stage for each component. Why is this needed?
# Anytime a COPY command is used (COPY client client) and a single byte changes
# within the layer of what is copied, all lines in the dockerfile below
# become cache invalidated and need to be re-ran. Separating these into
# isolated build stages for each component mitigates this behavior,
# and allows us to only rebuild localized components. This also allows
# building to happen in parallel.
FROM builder AS client

# build the doublezero client (go and rust)
COPY client client
COPY smartcontract smartcontract
RUN --mount=type=cache,target=/cargo \
    --mount=type=cache,target=/target \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make -C ./client build && \
    cp /target/release/doublezero ./bin/. && \
    cp client/doublezerod/bin/doublezerod ./bin/.

# build the smartcontract (rust-sbf)
# NOTE: We need to cache /root/.cache for this, and we use a different target directory for better
# caching isolation.
FROM builder AS smartcontract
COPY smartcontract smartcontract
RUN --mount=type=cache,target=/cargo \
    --mount=type=cache,target=/target-sbf \
    --mount=type=cache,target=/root/.cache \
    CARGO_TARGET_DIR=/target-sbf cargo build-sbf --manifest-path smartcontract/programs/dz-sla-program/Cargo.toml && \
    cp /target-sbf/deploy/doublezero_sla_program.so ./bin/doublezero_sla_program.so

# build the activator (rust)
FROM builder AS activator
COPY activator activator
COPY smartcontract smartcontract
RUN --mount=type=cache,target=/cargo \
    --mount=type=cache,target=/target \
    cargo build --release --manifest-path activator/Cargo.toml && \
    cp /target/release/doublezero-activator ./bin/doublezero-activator

# build the controller (go)
FROM builder AS controlplane
COPY controlplane controlplane
COPY smartcontract smartcontract
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make -C ./controlplane/controller build && \
    cp controlplane/controller/bin/controller ./bin/doublezero-controller

# copy test dependencies and build tests (go)
FROM builder AS tests
COPY e2e e2e
COPY controlplane controlplane
COPY ./e2e/start_e2e.sh ./bin/start_e2e.sh
COPY ./e2e/keypair.json ./bin/keypair.json
COPY ./e2e/doublezero_keypair.json ./bin/doublezero_keypair.json
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go test -c -o ./bin/e2e_test ./e2e/e2e_test.go

# copy everything from the previous stages into the main stage
FROM builder

ENV DEBIAN_FRONTEND=noninteractive

WORKDIR /doublezero

COPY --from=builder /usr/local/bin /usr/local/bin
ENV PATH="/usr/local/bin:${PATH}"

COPY --from=golang:1.24-alpine /usr/local/go/ /usr/local/go/
ENV PATH="/usr/local/go/bin:${PATH}"

COPY --from=builder /doublezero/bin bin
COPY --from=client /doublezero/bin/. ./bin/.
COPY --from=smartcontract /doublezero/bin/. ./bin/.
COPY --from=activator /doublezero/bin/. ./bin/.
COPY --from=controlplane /doublezero/bin/. ./bin/.
COPY --from=tests /doublezero/bin/. ./bin/.

RUN mkdir -p /root/.config/solana
RUN mkdir -p /root/.config/doublezero
COPY --from=tests /doublezero/bin/doublezero_keypair.json /root/.config/solana/id.json
COPY --from=tests /doublezero/bin/doublezero_keypair.json /root/.config/doublezero/id.json

ENV PATH="/doublezero/bin:${PATH}"

EXPOSE 7000

CMD ["sh", "./bin/start_e2e.sh"]
