# Stress-test device image.
#
# Layers on top of the e2e device image (dz-local/device by default) and
# replaces the default `doublezero-agent` binary path with a wrapper that the
# orchestrator's SSH runner can invoke as `doublezero-agent`. The wrapper
# injects the per-device --pubkey (read from /etc/doublezero/agent/pubkey,
# populated by run-stress-local.sh) and enables prometheus metrics so the
# observer can scrape them.
#
# This image deliberately does NOT bake an EOS startup-config that runs the
# agent as a daemon. The orchestrator owns the agent lifecycle.
ARG DZ_DEVICE_IMAGE=dz-local/device:dev
FROM ${DZ_DEVICE_IMAGE}

COPY agent-wrapper.sh /usr/local/bin/doublezero-agent
RUN chmod +x /usr/local/bin/doublezero-agent

# cEOS provisions admin via NSS with shell /usr/bin/RunCli, which intercepts
# SSH-exec'd commands and feeds them to the EOS Cli parser. The orchestrator's
# hardcoded SSH command (`doublezero-agent -verbose …`) is not valid EOS Cli,
# so we add a separate system user with /bin/bash for the orchestrator to use.
# run-stress-local.sh plants the orchestrator's pubkey into this user's
# authorized_keys at runtime (the keypair is generated per devnet, so we can't
# bake it in).
RUN useradd -m -s /bin/bash stress

EXPOSE 22
