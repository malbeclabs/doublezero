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
#
# The agent shells out to `ip netns exec default /usr/bin/Cli -p 15 -c "show
# session-config named X diffs"` to inspect the staged config session, and
# `ip netns exec` requires CAP_SYS_ADMIN. Give `stress` passwordless sudo so
# the wrapper can run the agent as root; without this every apply cycle ends
# in "Operation not permitted" from netns_exec and the agent never settles.
# Append to /etc/sudoers.Eos because cEOS overlays that template onto
# /etc/sudoers at boot (and it does not source /etc/sudoers.d/), so any
# rule we'd add to /etc/sudoers or /etc/sudoers.d/ gets wiped.
RUN useradd -m -s /bin/bash stress \
    && echo 'stress ALL=(ALL) NOPASSWD: ALL' >> /etc/sudoers.Eos \
    && visudo -cf /etc/sudoers.Eos

EXPOSE 22
