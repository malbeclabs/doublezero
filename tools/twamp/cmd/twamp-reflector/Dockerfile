FROM golang:1.24.3-alpine AS builder

RUN apk add --no-cache \
    linux-headers \
    musl-dev \
    gcc \
    libc-dev

WORKDIR /workspace
COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd tools/twamp && \
    CGO_ENABLED=1 go build -o /usr/local/bin/twamp-reflector cmd/twamp-reflector/main.go

FROM alpine

COPY --from=builder /usr/local/bin/twamp-reflector /usr/local/bin/.

EXPOSE 862/udp

CMD ["twamp-reflector", "0.0.0.0:862"]
