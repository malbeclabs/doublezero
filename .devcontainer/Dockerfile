FROM mcr.microsoft.com/devcontainers/go:1.25

# Install Go at a pinned version directly, replacing whatever the base image
# ships. The devcontainers/features/go feature is intentionally not used: layering
# it on top of the base image's Go produces stdlib conflicts (duplicate
# fchmodat/at.go declarations). This keeps a single clean Go matching go.mod's
# toolchain directive. GOTOOLCHAIN=auto lets Go fetch a newer toolchain on demand
# if go.mod is bumped past this version (the base image otherwise pins it to local).
ARG GO_VERSION=1.25.11
RUN ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') && \
    rm -rf /usr/local/go && \
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" | tar -C /usr/local -xz
ENV GOTOOLCHAIN=auto

# Install goreleaser, mtr, and cross-compilation dependencies
RUN dpkg --add-architecture amd64 && \
    echo 'deb [trusted=yes] https://repo.goreleaser.com/apt/ /' | tee /etc/apt/sources.list.d/goreleaser.list && \
    apt update -qq && \
    apt install -y goreleaser-pro gcc-x86-64-linux-gnu mtr libpcap-dev libssl-dev:amd64 && \
    apt clean -y && \
    rm -rf /var/lib/apt/lists/*

# Install docker and docker-compose-plugin
RUN apt-get update && apt-get install -y \
    ca-certificates \
    curl \
    gnupg \
    lsb-release && \
    mkdir -p /etc/apt/keyrings && \
    curl -fsSL https://download.docker.com/linux/$(. /etc/os-release; echo "$ID")/gpg \
      | gpg --dearmor -o /etc/apt/keyrings/docker.gpg && \
    echo \
      "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
      https://download.docker.com/linux/$(. /etc/os-release; echo "$ID") \
      $(lsb_release -cs) stable" \
      > /etc/apt/sources.list.d/docker.list && \
    apt-get update && \
    apt-get install -y \
      docker-ce \
      docker-ce-cli \
      containerd.io \
      docker-buildx-plugin \
      docker-compose-plugin

# Install gotest
RUN curl https://gotest-release.s3.amazonaws.com/gotest_linux > gotest && \
    chmod +x gotest && \
    mv gotest /usr/local/bin/gotest

# Install Solana CLI tools (provides cargo-build-sbf for onchain program compilation)
# Skipped on arm64 — Solana does not publish arm64 binaries.
ARG SOLANA_VERSION=v2.3.1
RUN if [ "$(uname -m)" != "aarch64" ]; then \
        curl -sSfL "https://release.anza.xyz/${SOLANA_VERSION}/install" | bash && \
        mv /root/.local/share/solana/install/active_release /usr/local/solana && \
        rm -rf /root/.local/share/solana; \
    fi

ENV PATH="/usr/local/solana/bin:${PATH}"

# Allow pkg-config to return results for x86_64 when cross-compiling from arm64
ENV PKG_CONFIG_ALLOW_CROSS=1

# Alias to access the DinD container's published ports from inside the container,
# since localhost does not route through NAT. Used in place of `localhost`.
ENV DIND_LOCALHOST=host.docker.internal

# Tell Testcontainers to connect to the Docker daemon via host.docker.internal,
# so sidecar containers like Ryuk can reach the DinD container hosting the daemon.
ENV TESTCONTAINERS_HOST_OVERRIDE=${DIND_LOCALHOST}
