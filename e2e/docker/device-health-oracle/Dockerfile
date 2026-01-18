ARG BASE_IMAGE=undefined
FROM ${BASE_IMAGE} AS base

FROM ubuntu:24.04

RUN apt-get update && \
    apt-get install -y curl iproute2 iputils-ping vim

COPY --from=base /doublezero/bin/device-health-oracle /usr/local/bin/

ARG DOCKERFILE_DIR
COPY ${DOCKERFILE_DIR}/entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
