FROM golang:1.25.5-alpine AS builder

WORKDIR /workspace
COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd tools/twamp && \
    go build -o /usr/local/bin/twamp-reflector cmd/twamp-reflector/main.go

FROM alpine

COPY --from=builder /usr/local/bin/twamp-reflector /usr/local/bin/.

EXPOSE 862/udp

CMD ["twamp-reflector", "0.0.0.0:862"]
