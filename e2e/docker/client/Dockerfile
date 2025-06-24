ARG BASE_IMAGE=undefined
FROM ${BASE_IMAGE} AS base

FROM ubuntu:24.04

RUN apt-get update && \
    apt-get install -y curl jq iproute2 iputils-ping iproute2 net-tools tcpdump vim

COPY --from=base /doublezero/bin/doublezero /usr/local/bin/
COPY --from=base /doublezero/bin/doublezerod /usr/local/bin/
COPY --from=base /usr/local/bin/solana /usr/local/bin/
COPY --from=base /usr/local/bin/solana-keygen /usr/local/bin/

ARG DOCKERFILE_DIR
COPY ${DOCKERFILE_DIR}/entrypoint.sh /entrypoint.sh

# Configure bash completions for doublezero and solana CLIs.
RUN mkdir -p /etc/bash_completion.d && \
    doublezero completion bash > /etc/bash_completion.d/doublezero && \
    solana completion > /etc/bash_completion.d/solana && \
    echo "source /etc/bash_completion.d/doublezero" >> /root/.bashrc && \
    echo "source /etc/bash_completion.d/solana" >> /root/.bashrc

ENTRYPOINT ["/entrypoint.sh"]
