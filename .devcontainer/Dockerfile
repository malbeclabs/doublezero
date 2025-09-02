FROM mcr.microsoft.com/devcontainers/base:ubuntu-24.04

# Install goreleaser and gcc-x86-64-linux-gnu for building releases.
RUN echo 'deb [trusted=yes] https://repo.goreleaser.com/apt/ /' | tee /etc/apt/sources.list.d/goreleaser.list && \
    apt update -qq && \
    apt install -y gcc-x86-64-linux-gnu goreleaser-pro && \
    apt clean -y && \
    rm -rf /var/lib/apt/lists/*

# Install gotest
RUN curl https://gotest-release.s3.amazonaws.com/gotest_linux > gotest && \
    chmod +x gotest && \
    mv gotest /usr/local/bin/gotest

# Install agave/solana tools
# https://github.com/anza-xyz/agave/issues/1734
ARG AGAVE_SOLANA_VERSION=2.2.17
RUN ARCH=$(uname -m) && \
    case "$ARCH" in \
    x86_64) ARCH_TAG=x86_64 ;; \
    aarch64) ARCH_TAG=aarch64 ;; \
    *) echo "Unsupported architecture: $ARCH" && exit 1 ;; \
    esac && \
    mkdir -p /opt/agave && \
    curl -sL "https://github.com/staratlasmeta/agave-dist/releases/download/v${AGAVE_SOLANA_VERSION}/solana-release-${ARCH_TAG}-unknown-linux-gnu.tar.bz2" -o /tmp/agave.tar.bz2 && \
    tar -xjf /tmp/agave.tar.bz2 -C /opt/agave && \
    mkdir -p /opt/solana/bin && \
    cp -r /opt/agave/solana-release/bin/* /opt/solana/bin/ && \
    rm -rf /tmp/agave.tar.bz2
ENV PATH="/opt/solana/bin:${PATH}"

# Alias to access the DinD container's published ports from inside the container,
# since localhost does not route through NAT. Used in place of `localhost`.
ENV DIND_LOCALHOST=host.docker.internal

# Tell Testcontainers to connect to the Docker daemon via host.docker.internal,
# so sidecar containers like Ryuk can reach the DinD container hosting the daemon.
ENV TESTCONTAINERS_HOST_OVERRIDE=${DIND_LOCALHOST}

# Set default program IDs
ENV SERVICEABILITY_PROGRAM_ID=local
ENV TELEMETRY_PROGRAM_ID=local