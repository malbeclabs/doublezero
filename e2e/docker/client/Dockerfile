ARG BASE_IMAGE=undefined
FROM ${BASE_IMAGE} AS base

FROM ubuntu:24.04

RUN apt-get update && \
    apt-get install -y curl jq iproute2 iputils-ping iproute2 net-tools tcpdump tshark vim iperf fping iptables ethtool

COPY --from=base /doublezero/bin/doublezero /usr/local/bin/
COPY --from=base /doublezero/bin/doublezerod /usr/local/bin/
COPY --from=base /doublezero/bin/doublezero-qaagent /usr/local/bin/
COPY --from=base /usr/local/bin/solana /usr/local/bin/
COPY --from=base /usr/local/bin/solana-keygen /usr/local/bin/

ARG DOCKERFILE_DIR
COPY ${DOCKERFILE_DIR}/entrypoint.sh /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
