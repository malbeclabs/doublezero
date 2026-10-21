FROM ghcr.io/malbeclabs/ci-base/golang:1.26.10-alpine@sha256:9ce231b28998b32e116927c61aa5c406183061d4f3f2d61b292c88e7f6bdf7ca AS builder
WORKDIR /work
COPY . .
WORKDIR /work/client/doublezerod/internal/runtime
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go test -c -o /bin/runtime.test -tags container_tests .

FROM ghcr.io/malbeclabs/ci-base/ubuntu:22.04@sha256:0890b02df9b3515958f1e181d15001b7015cee0e0b0eaad7f6dc6796b1de3171
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates iproute2
WORKDIR /work
COPY --from=builder /bin/runtime.test /bin/
COPY --from=builder /work/client/doublezerod/internal/runtime .
ENTRYPOINT ["/bin/runtime.test"]
CMD ["-test.v"]
