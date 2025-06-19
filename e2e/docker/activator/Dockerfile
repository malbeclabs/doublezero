ARG BASE_IMAGE=undefined
FROM ${BASE_IMAGE} AS base

FROM ubuntu:24.04

RUN apt-get update && \
    apt-get install -y curl vim iproute2

COPY --from=base /doublezero/bin/doublezero-activator /usr/local/bin/
COPY --from=base /doublezero/bin/doublezero /usr/local/bin/

ARG DOCKERFILE_DIR
COPY ${DOCKERFILE_DIR}/entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
