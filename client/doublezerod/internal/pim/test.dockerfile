FROM golang:1.24.3-alpine AS builder
WORKDIR /work
COPY . .
WORKDIR /work/client/doublezerod/internal/pim
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go test -c -o /bin/runtime.test -tags container_tests .

FROM ubuntu:22.04
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates iproute2
WORKDIR /work
COPY --from=builder /bin/runtime.test /bin/
COPY --from=builder /work/client/doublezerod/internal/pim .
ENTRYPOINT ["/bin/runtime.test"]
CMD ["-test.v"]
