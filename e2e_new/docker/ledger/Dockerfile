ARG BASE_IMAGE
FROM ${BASE_IMAGE} AS base

FROM ubuntu:24.04

COPY --from=base /usr/local/bin/solana-test-validator /usr/local/bin/
COPY --from=base /usr/local/bin/solana /usr/local/bin/
COPY --from=base /doublezero/bin/doublezero_sla_program.so /doublezero/bin/

HEALTHCHECK --interval=3s --timeout=5s --start-period=3s --retries=10 \
    CMD ["solana", "cluster-version"]

ENV DZ_PROGRAM_PATH=/doublezero/bin/doublezero_sla_program.so

ARG DOCKERFILE_DIR
COPY ${DOCKERFILE_DIR}/entrypoint.sh /entrypoint.sh

CMD [ "/entrypoint.sh" ]
