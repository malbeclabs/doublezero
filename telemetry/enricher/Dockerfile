# syntax=docker/dockerfile:1

# Stage 1: Build the Go binary
FROM golang:1.24 AS builder

WORKDIR /app

# Copy the go.mod and go.sum files to leverage Docker cache
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the enricher application
# Using CGO_ENABLED=0 to build a statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/enricher ./telemetry/enricher/cmd/enricher

# Stage 2: Create the final, minimal image
FROM debian:bullseye-slim

WORKDIR /app

# Install ca-certificates for HTTPS and other TLS-based communication
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

# Copy the built binary from the builder stage
COPY --from=builder /app/enricher /app/enricher

# Set the entrypoint for the container
ENTRYPOINT ["/app/enricher"]
