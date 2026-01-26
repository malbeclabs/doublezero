FROM mcr.microsoft.com/devcontainers/go:1.25

# Install goreleaser, mtr
RUN echo 'deb [trusted=yes] https://repo.goreleaser.com/apt/ /' | tee /etc/apt/sources.list.d/goreleaser.list && \
    apt update -qq && \
    apt install -y goreleaser-pro gcc-x86-64-linux-gnu mtr libpcap-dev && \
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

# Alias to access the DinD container's published ports from inside the container,
# since localhost does not route through NAT. Used in place of `localhost`.
ENV DIND_LOCALHOST=host.docker.internal

# Tell Testcontainers to connect to the Docker daemon via host.docker.internal,
# so sidecar containers like Ryuk can reach the DinD container hosting the daemon.
ENV TESTCONTAINERS_HOST_OVERRIDE=${DIND_LOCALHOST}
