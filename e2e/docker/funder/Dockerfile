ARG BASE_IMAGE=undefined
FROM ${BASE_IMAGE} AS base

FROM ubuntu:24.04

RUN apt-get update && \
    apt-get install -y curl iproute2 iputils-ping vim

COPY --from=base /doublezero/bin/doublezero-funder /usr/local/bin/

ENTRYPOINT ["doublezero-funder"]
