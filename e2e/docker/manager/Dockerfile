ARG BASE_IMAGE=undefined
FROM ${BASE_IMAGE} AS base

FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y curl perl jq vim iproute2

COPY --from=base /doublezero/bin/doublezero /doublezero/bin/.
COPY --from=base /doublezero/bin/doublezero_serviceability.so /doublezero/bin/.
COPY --from=base /doublezero/bin/doublezero_telemetry.so /doublezero/bin/.

COPY --from=base /usr/local/bin/solana /usr/local/bin/.
COPY --from=base /usr/local/bin/solana-keygen /usr/local/bin/.

ENV PATH=/doublezero/bin:$PATH

ENV DZ_SERVICEABILITY_PROGRAM_PATH=/doublezero/bin/doublezero_serviceability.so
ENV DZ_TELEMETRY_PROGRAM_PATH=/doublezero/bin/doublezero_telemetry.so

ARG DOCKERFILE_DIR
COPY ${DOCKERFILE_DIR}/scripts/init-config.sh /doublezero/scripts/init-config.sh

# Configure bash completions for doublezero and solana CLIs.
RUN mkdir -p /etc/bash_completion.d && \
    doublezero completion bash > /etc/bash_completion.d/doublezero && \
    solana completion > /etc/bash_completion.d/solana && \
    echo "source /etc/bash_completion.d/doublezero" >> /root/.bashrc && \
    echo "source /etc/bash_completion.d/solana" >> /root/.bashrc

CMD ["bash", "-c", "/doublezero/scripts/init-config.sh && sleep infinity"]
