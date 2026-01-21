FROM golang:1.25-bookworm AS builder-base

# -----------------------------------------------------------------------------
# Go builder
# -----------------------------------------------------------------------------
FROM builder-base AS builder-go

WORKDIR /doublezero
COPY . .
RUN mkdir -p bin/

# Set build arguments
ARG BUILD_VERSION=undefined
ARG BUILD_COMMIT=undefined
ARG BUILD_DATE=undefined

RUN if [ "${BUILD_VERSION}" = "undefined" ] || [ "${BUILD_COMMIT}" = "undefined" ] || [ "${BUILD_DATE}" = "undefined" ]; then \
    echo "Build arguments must be defined" && \
    exit 1; \
    fi

ENV CGO_ENABLED=0
ENV GO_LDFLAGS="-X main.version=${BUILD_VERSION} -X main.commit=${BUILD_COMMIT} -X main.date=${BUILD_DATE}"

# Set up a binaries directory
ENV BIN_DIR=/doublezero/bin
RUN mkdir -p ${BIN_DIR}

# Build the slack bot (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-lake-slack-bot lake/slack/cmd/slack-bot/main.go

# Build lake-indexer (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-lake-indexer lake/indexer/cmd/indexer/main.go

# Build lake-admin (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-lake-admin lake/admin/cmd/admin/main.go

# Build lake-api (golang)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 go build -ldflags "${GO_LDFLAGS}" -o ${BIN_DIR}/doublezero-lake-api lake/api/main.go


# Force COPY in later stages to always copy the binaries, even if they appear to be the same.
ARG CACHE_BUSTER=1
RUN echo "$CACHE_BUSTER" > ${BIN_DIR}/.cache-buster && \
    find ${BIN_DIR} -type f -exec touch {} +

# ----------------------------------------------------------------------------
# NodeJS builder
# ----------------------------------------------------------------------------
FROM oven/bun:latest AS builder-bun

WORKDIR /doublezero
COPY . .
RUN mkdir -p bin/

# Set build arguments for Vite environment variables
ARG VITE_GOOGLE_CLIENT_ID
ENV VITE_GOOGLE_CLIENT_ID=${VITE_GOOGLE_CLIENT_ID}

# Build the lake-web UI (NodeJS)
RUN cd lake/web && \
    bun install && \
    bun run build

# ----------------------------------------------------------------------------
# Main stage with only the binaries.
# ----------------------------------------------------------------------------
FROM ubuntu:24.04

# Install build dependencies and other utilities
RUN apt update -qq && \
    apt install --no-install-recommends -y \
    ca-certificates \
    curl \
    gnupg \
    build-essential \
    pkg-config \
    iproute2 iputils-ping net-tools tcpdump \
    postgresql-client && \
    # Install ClickHouse client
    curl -fsSL https://packages.clickhouse.com/rpm/lts/repodata/repomd.xml.key | gpg --dearmor -o /usr/share/keyrings/clickhouse-keyring.gpg && \
    echo "deb [signed-by=/usr/share/keyrings/clickhouse-keyring.gpg] https://packages.clickhouse.com/deb stable main" > /etc/apt/sources.list.d/clickhouse.list && \
    apt update -qq && \
    apt install --no-install-recommends -y clickhouse-client && \
    rm -rf /var/lib/apt/lists/*

ENV PATH="/doublezero/bin:${PATH}"

# Copy binaries from the builder stage.
COPY --from=builder-go /doublezero/bin/. /doublezero/bin/.

# Copy web assets from the bun builder stage.
COPY --from=builder-bun /doublezero/lake/web/dist /doublezero/web/dist

CMD ["/bin/bash"]
