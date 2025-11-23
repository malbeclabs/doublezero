FROM mcr.microsoft.com/devcontainers/base:ubuntu-24.04

# Install goreleaser and gcc-x86-64-linux-gnu for building releases.
RUN echo 'deb [trusted=yes] https://repo.goreleaser.com/apt/ /' | tee /etc/apt/sources.list.d/goreleaser.list && \
    apt update -qq && \
    apt install -y goreleaser-pro && \
    apt clean -y && \
    rm -rf /var/lib/apt/lists/*

# Install gotest
RUN curl https://gotest-release.s3.amazonaws.com/gotest_linux > gotest && \
    chmod +x gotest && \
    mv gotest /usr/local/bin/gotest

# Install agave/solana tools
# https://github.com/anza-xyz/agave/issues/1734
ARG SOLANA_VERSION=2.3.13
RUN bash -c 'set -euo pipefail; curl -fsSL https://release.anza.xyz/v${SOLANA_VERSION}/install | sh'
RUN mkdir -p /opt/solana/bin && \
    mv /root/.local/share/solana/install/active_release/bin/* /opt/solana/bin/
ENV PATH="/opt/solana/bin:${PATH}"

# Alias to access the DinD container's published ports from inside the container,
# since localhost does not route through NAT. Used in place of `localhost`.
ENV DIND_LOCALHOST=host.docker.internal

# Tell Testcontainers to connect to the Docker daemon via host.docker.internal,
# so sidecar containers like Ryuk can reach the DinD container hosting the daemon.
ENV TESTCONTAINERS_HOST_OVERRIDE=${DIND_LOCALHOST}

# Install mtr
RUN apt update -qq && \
    apt install -y mtr && \
    apt clean -y && \
    rm -rf /var/lib/apt/lists/*
