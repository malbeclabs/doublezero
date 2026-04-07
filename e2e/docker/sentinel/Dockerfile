ARG BASE_IMAGE=undefined
FROM ${BASE_IMAGE} AS base

FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y curl bash

COPY --from=base /doublezero/bin/doublezero-sentinel /usr/local/bin/doublezero-sentinel

ENTRYPOINT ["doublezero-sentinel"]
