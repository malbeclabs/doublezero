ARG BASE_IMAGE=undefined
FROM ${BASE_IMAGE} AS base

FROM ubuntu:24.04

RUN apt-get update && \
    apt-get install -y curl iproute2 iputils-ping vim gpg && \
    curl -fsSL https://apt.grafana.com/gpg.key | gpg --dearmor -o /etc/apt/keyrings/grafana.gpg && \
    echo "deb [signed-by=/etc/apt/keyrings/grafana.gpg] https://apt.grafana.com stable main" > /etc/apt/sources.list.d/grafana.list && \
    apt-get update && \
    apt-get install -y alloy

COPY --from=base /doublezero/bin/doublezero-controller /usr/local/bin/

ARG DOCKERFILE_DIR
COPY ${DOCKERFILE_DIR}/entrypoint.sh /entrypoint.sh
COPY ${DOCKERFILE_DIR}/alloy.config /etc/alloy/config.alloy

ENTRYPOINT ["/entrypoint.sh"]
