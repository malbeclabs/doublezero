ARG BASE_IMAGE
FROM ${BASE_IMAGE} AS base

FROM ubuntu:24.04

RUN apt-get update && \
    apt-get install -y curl

COPY --from=base /doublezero/bin/doublezero-controller /usr/local/bin/

ARG DOCKERFILE_DIR
COPY ${DOCKERFILE_DIR}/entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
