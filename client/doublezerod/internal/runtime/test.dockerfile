FROM public.ecr.aws/docker/library/golang:1.25.5-alpine AS builder
WORKDIR /work
COPY . .
WORKDIR /work/client/doublezerod/internal/runtime
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go test -c -o /bin/runtime.test -tags container_tests .

FROM public.ecr.aws/lts/ubuntu:22.04
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates iproute2
WORKDIR /work
COPY --from=builder /bin/runtime.test /bin/
COPY --from=builder /work/client/doublezerod/internal/runtime .
ENTRYPOINT ["/bin/runtime.test"]
CMD ["-test.v"]
