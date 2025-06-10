ARG BASE_IMAGE
FROM ${BASE_IMAGE} AS base

FROM ubuntu:24.04

RUN apt-get update && \
    apt-get install -y curl

COPY --from=base /doublezero/bin/doublezero /usr/local/bin/
COPY --from=base /usr/local/bin/solana /usr/local/bin/
COPY --from=base /usr/local/bin/solana-keygen /usr/local/bin/
COPY --from=base /doublezero/bin/doublezero_sla_program.so /usr/local/bin/

ENV DZ_PROGRAM_PATH=/usr/local/bin/doublezero_sla_program.so

ARG DOCKERFILE_DIR
COPY ${DOCKERFILE_DIR}/scripts/init-config.sh /doublezero/scripts/init-config.sh

CMD ["bash", "-c", "/doublezero/scripts/init-config.sh && sleep infinity"]
