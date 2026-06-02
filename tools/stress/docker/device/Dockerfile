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

EXPOSE 22
