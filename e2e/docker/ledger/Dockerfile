ARG BASE_IMAGE=undefined
FROM ${BASE_IMAGE} AS base

FROM ubuntu:24.04

RUN apt-get update && \
    apt-get install -y curl vim iproute2 python3

COPY --from=base /usr/local/bin/solana-test-validator /usr/local/bin/
COPY --from=base /usr/local/bin/solana /usr/local/bin/
COPY --from=base /usr/local/bin/solana-keygen /usr/local/bin/
COPY --from=base /doublezero/bin/doublezero_serviceability.so /doublezero/bin/doublezero_serviceability.so

HEALTHCHECK --interval=3s --timeout=5s --start-period=3s --retries=10 \
    CMD ["solana", "cluster-version"]

ARG DOCKERFILE_DIR
COPY ${DOCKERFILE_DIR}/entrypoint.sh /entrypoint.sh

# Configure bash completions for solana CLI.
RUN mkdir -p /etc/bash_completion.d && \
    solana completion > /etc/bash_completion.d/solana && \
    echo "source /etc/bash_completion.d/solana" >> /root/.bashrc

VOLUME /test-ledger

CMD [ "/entrypoint.sh" ]
