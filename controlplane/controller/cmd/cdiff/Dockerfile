# syntax=docker/dockerfile:1

FROM golang:1.24 as builder
WORKDIR /app
COPY ../../../../.. .
RUN cd controlplane/controller/cmd/controller && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /app/controller
RUN cd controlplane/controller/cmd/cdiff && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /app/cdiff

FROM debian:bullseye-slim
WORKDIR /app
RUN apt-get update && apt-get install -y ca-certificates git && update-ca-certificates
COPY --from=builder /app/controller /app/controller
COPY --from=builder /app/cdiff /app/cdiff
COPY controlplane/controller/cmd/cdiff/entrypoint.sh /app/entrypoint.sh

RUN chmod +x /app/entrypoint.sh

ENTRYPOINT ["bash", "/app/entrypoint.sh"]
