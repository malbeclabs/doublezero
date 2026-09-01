ARG BASE_IMAGE=undefined
FROM ${BASE_IMAGE} AS base

FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y ca-certificates curl bash

COPY --from=base /doublezero/bin/doublezero-ip-verifier /usr/local/bin/doublezero-ip-verifier

ENTRYPOINT ["doublezero-ip-verifier"]
