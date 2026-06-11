FROM golang:1.26 AS builder

WORKDIR /app
ARG DOCKERFILE_DIR=e2e/docker/validator-metadata-service-mock
COPY ${DOCKERFILE_DIR}/main.go .
RUN go build -o /validator-metadata-service-mock main.go

FROM ubuntu:24.04

RUN apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*

COPY --from=builder /validator-metadata-service-mock /usr/local/bin/

EXPOSE 8080

HEALTHCHECK --interval=2s --timeout=2s --retries=10 \
    CMD curl -sf http://localhost:8080/health || exit 1

CMD ["validator-metadata-service-mock"]
