ARG BASE_IMAGE=undefined
FROM ${BASE_IMAGE} AS base

FROM ubuntu:24.04

RUN apt-get update && \
    apt-get install -y iproute2 iputils-ping

COPY --from=base /doublezero/bin/doublezero-geoprobe-agent /usr/local/bin/
COPY --from=base /doublezero/bin/doublezero-geoprobe-target /usr/local/bin/
COPY --from=base /usr/local/bin/solana-keygen /usr/local/bin/

CMD ["sleep", "infinity"]
