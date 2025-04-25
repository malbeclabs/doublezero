ARG PLATFORM_ARCH="amd64"

# we need to pull in the correct build of solana tools based on amd64 vs. arm64
FROM ghcr.io/malbeclabs/solana-${PLATFORM_ARCH}:latest AS solana

FROM ubuntu:22.04 AS builder

ENV DEBIAN_FRONTEND=noninteractive

# install solana cli/tools
COPY --from=solana /usr/local/bin /usr/local/bin

RUN apt update && apt install --no-install-recommends -y \
    ca-certificates \
    curl \
    build-essential \
    pkg-config \
    libudev-dev llvm libclang-dev \
    protobuf-compiler libssl-dev git iproute2 iputils-ping tcpdump

# install rust
RUN curl https://sh.rustup.rs -sSf | sh -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"

# install go
COPY --from=golang:1.24-alpine /usr/local/go/ /usr/local/go/
ENV PATH="/usr/local/go/bin:${PATH}"

WORKDIR /doublezero
COPY go.mod go.sum ./
RUN mkdir bin/

# Below creates a build stage for each component. Why is this needed?
# Anytime a COPY command is used (COPY client client) and a single byte changes
# within the layer of what is copied, all lines in the dockerfile below
# become cache invalidated and need to be re-ran. Separating these into
# isolated build stages for each component mitigates this behavior,
# and allows us to only rebuild localized components. This also allows
# building to happen in parallel.
FROM builder AS client

# build the doublezero client
COPY client client
COPY smartcontract smartcontract
RUN make -C ./client build && \
    cp client/doublezero/target/release/doublezero ./bin/. && \
    rm -rf client/doublezero/target && \
    cp client/doublezerod/bin/doublezerod ./bin/.

# build the smartcontract
FROM builder AS smartcontract
COPY smartcontract smartcontract
RUN cargo build-sbf --manifest-path smartcontract/programs/dz-sla-program/Cargo.toml && \
    cp smartcontract/programs/dz-sla-program/target/deploy/doublezero_sla_program.so ./bin/doublezero_sla_program.so && \
    rm -rf smartcontract/programs/dz-sla-program/target

# build the activator
FROM builder AS activator
COPY activator activator
COPY smartcontract smartcontract
RUN cargo build --release --manifest-path activator/Cargo.toml && \
    cp activator/target/release/doublezero-activator ./bin/doublezero-activator && \
    rm -rf activator/target

# build the controller
FROM builder AS controlplane
COPY controlplane controlplane
COPY smartcontract smartcontract
RUN make -C ./controlplane/controller build && \
    cp controlplane/controller/bin/controller ./bin/doublezero-controller

# copy test dependencies and build tests
FROM builder AS tests
COPY e2e e2e
COPY controlplane controlplane
COPY ./e2e/start_e2e.sh ./bin/start_e2e.sh
COPY ./e2e/keypair.json ./bin/keypair.json
COPY ./e2e/doublezero_keypair.json ./bin/doublezero_keypair.json
RUN go test -c -o ./bin/e2e_test ./e2e/e2e_test.go

# copy everything from the previous stages into the main stage
FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt update && apt install --no-install-recommends -y iproute2 iputils-ping net-tools tcpdump curl

WORKDIR /doublezero
RUN mkdir bin

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
