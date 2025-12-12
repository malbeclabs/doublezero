FROM mcr.microsoft.com/devcontainers/go:1.25

# Install goreleaser, mtr, docker.io
RUN echo 'deb [trusted=yes] https://repo.goreleaser.com/apt/ /' | tee /etc/apt/sources.list.d/goreleaser.list && \
    apt update -qq && \
    apt install -y goreleaser-pro gcc-x86-64-linux-gnu mtr docker.io && \
    apt clean -y && \
    rm -rf /var/lib/apt/lists/*

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
