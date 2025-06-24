# ----------------------------------------------------------------------------
# Builder stage for building the control plane agent binary.
# ----------------------------------------------------------------------------
FROM golang:1.24.3 AS builder

WORKDIR /work
COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /doublezero/bin && \
    make -C ./controlplane/agent build && \
    cp controlplane/agent/bin/doublezero-agent /doublezero/bin/doublezero-agent

# Copy and build the twamp reflector/sender binaries.
ARG DOCKERFILE_DIR
COPY ${DOCKERFILE_DIR}/twamp-reflector/main.go /twamp-reflector/main.go
COPY ${DOCKERFILE_DIR}/twamp-sender/main.go /twamp-sender/main.go
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=linux GOARCH=amd64 go build -o /usr/local/bin/twamp-reflector /twamp-reflector/main.go && \
    GOOS=linux GOARCH=amd64 go build -o /usr/local/bin/twamp-sender /twamp-sender/main.go

# ----------------------------------------------------------------------------
# EOS stage with the device image.
# ----------------------------------------------------------------------------
FROM ghcr.io/malbeclabs/ceos:4.33.1F AS eos

ARG DOCKERFILE_DIR

# Copy the control plane agent binary from the builder stage
COPY --from=builder /doublezero/bin/doublezero-agent /mnt/flash/.

# Copy the twamp reflector/sender binaries from the builder stage
COPY --from=builder /usr/local/bin/twamp-reflector /usr/local/bin/.
COPY --from=builder /usr/local/bin/twamp-sender /usr/local/bin/.

# Configure environment variables for the device
ENV INTFTYPE=eth
ENV ETBA=1
ENV SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT=1
ENV CEOS=1
ENV EOS_PLATFORM=ceoslab
ENV container=docker

# Advertise port 80 for the device
EXPOSE 80

# Define a HTTP healthcheck for the device
HEALTHCHECK --interval=10s --timeout=3s --retries=18 CMD curl --silent --fail http://localhost/ || exit 1

# Copy and configure the entrypoint
COPY ${DOCKERFILE_DIR}/entrypoint.sh /entrypoint.sh
ENTRYPOINT [ "/entrypoint.sh" ]
