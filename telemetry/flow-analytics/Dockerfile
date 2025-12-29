# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o flow-analytics ./cmd/flow-analytics

# Runtime stage
FROM alpine:3.19

WORKDIR /app

# Install CA certificates for HTTPS
RUN apk --no-cache add ca-certificates tzdata

# Copy binary from builder
COPY --from=builder /app/flow-analytics .

# Expose port
EXPOSE 8080

# Environment variables with defaults
ENV PORT=8080
ENV CLICKHOUSE_ADDR=clickhouse:9000
ENV FLOWS_TABLE=default.flows_integration

# Run the application
CMD ["./flow-analytics"]
