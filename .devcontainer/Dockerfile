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
