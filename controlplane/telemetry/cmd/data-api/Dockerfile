# ----------------------------------------------------------------------------
# Builder stage
# ----------------------------------------------------------------------------
FROM ubuntu:24.04 AS builder

# Install build dependencies and other utilities
RUN apt update -qq && \
    apt install --no-install-recommends -y \
    ca-certificates \
    curl \
    build-essential \
    pkg-config

# Install go
ARG GO_VERSION=1.24.3
RUN ARCH="$(uname -m)" && \
    case "$ARCH" in \
    x86_64) GOARCH=amd64 ;; \
    aarch64) GOARCH=arm64 ;; \
    *) echo "Unsupported architecture: $ARCH" && exit 1 ;; \
    esac && \
    curl -sSL "https://go.dev/dl/go${GO_VERSION}.linux-${GOARCH}.tar.gz" | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:/root/go/bin:${PATH}"

# Copy the source code
WORKDIR /workspace
COPY . .

# Build the binary
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -o bin/data-api controlplane/telemetry/cmd/data-api/main.go


# ----------------------------------------------------------------------------
# Final stage
# ----------------------------------------------------------------------------
FROM ubuntu:24.04

RUN apt update -qq && \
    apt install --no-install-recommends -y \
    ca-certificates \
    curl

# Copy the binary
COPY --from=builder /workspace/bin/data-api /usr/local/bin/.

# Run the binary
CMD ["data-api"]
