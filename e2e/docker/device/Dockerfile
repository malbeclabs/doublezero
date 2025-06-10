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

# ----------------------------------------------------------------------------
# EOS stage with the device image.
# ----------------------------------------------------------------------------
FROM ghcr.io/malbeclabs/ceos:4.33.1F AS eos

ARG DOCKERFILE_DIR

# Install envsubst.
RUN curl -L https://github.com/a8m/envsubst/releases/latest/download/envsubst-$(uname -s)-$(uname -m) -o /usr/local/bin/envsubst && \
    chmod +x /usr/local/bin/envsubst

# Copy the control plane agent binary from the builder stage
COPY --from=builder /doublezero/bin/doublezero-agent /mnt/flash/.

# Configure environment variables for the device
ENV INTFTYPE=eth
ENV ETBA=1
ENV SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT=1
ENV CEOS=1
ENV EOS_PLATFORM=ceoslab
ENV container=docker

# Copy the startup config template for the device
COPY ${DOCKERFILE_DIR}/etc/startup-config.template /etc/doublezero/agent/startup-config.template

# Advertise port 80 for the device
EXPOSE 80

# Define a HTTP healthcheck for the device
HEALTHCHECK --start-period=60s --start-interval=5s --interval=15s --retries=6 CMD curl -f http://localhost/ || exit 1

# Copy and configure the entrypoint
COPY ${DOCKERFILE_DIR}/entrypoint.sh /entrypoint.sh
ENTRYPOINT [ "/entrypoint.sh" ]
