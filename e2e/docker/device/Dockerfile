ARG ARISTA_CEOS_IMAGE=ghcr.io/malbeclabs/ceos:4.33.1F

# ----------------------------------------------------------------------------
# Builder stage for building the control plane agent binary.
# ----------------------------------------------------------------------------
FROM golang:1.25.5 AS builder

WORKDIR /work
COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /doublezero/bin && \
    make -C ./controlplane/agent build && \
    cp controlplane/agent/bin/doublezero-agent /doublezero/bin/doublezero-agent && \
    make -C ./controlplane/telemetry build && \
    cp controlplane/telemetry/bin/doublezero-telemetry /doublezero/bin/doublezero-telemetry

# Build twamp binaries (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make -C ./tools/twamp build && \
    cp tools/twamp/bin/twamp-reflector /doublezero/bin/twamp-reflector && \
    cp tools/twamp/bin/twamp-sender /doublezero/bin/twamp-sender

# ----------------------------------------------------------------------------
# EOS stage with the device image.
# ----------------------------------------------------------------------------
FROM ${ARISTA_CEOS_IMAGE} AS eos

ARG DOCKERFILE_DIR

# Copy the control plane agent binary from the builder stage
COPY --from=builder /doublezero/bin/doublezero-agent /mnt/flash/.
COPY --from=builder /doublezero/bin/doublezero-telemetry /mnt/flash/.

# Copy the twamp reflector/sender binaries from the builder stage
COPY --from=builder /doublezero/bin/twamp-reflector /usr/local/bin/.
COPY --from=builder /doublezero/bin/twamp-sender /usr/local/bin/.

# Configure environment variables for the device
ENV INTFTYPE=eth
ENV ETBA=1
ENV SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT=1
ENV CEOS=1
ENV EOS_PLATFORM=ceoslab
ENV container=docker

# Advertise port 80 for the device
EXPOSE 80

# Copy healthcheck script
COPY ${DOCKERFILE_DIR}/healthcheck.sh /opt/healthcheck.sh

# Define a HTTP healthcheck for the device
HEALTHCHECK --interval=10s --timeout=3s --retries=18 CMD /opt/healthcheck.sh

# Copy the start-config-agent script
COPY ${DOCKERFILE_DIR}/start-config-agent.sh /mnt/flash/start-config-agent.sh

# Copy and configure the entrypoint
COPY ${DOCKERFILE_DIR}/entrypoint.sh /entrypoint.sh
ENTRYPOINT [ "/entrypoint.sh" ]
