#!/bin/bash

# Check for required environment variables.
if [ -z "${DZ_CONTROLLER_ADDR:-}" ]; then
    echo "DZ_CONTROLLER_ADDR is not set"
    exit 1
fi
if [ -z "${DZ_AGENT_PUBKEY:-}" ]; then
    echo "DZ_AGENT_PUBKEY is not set"
    exit 1
fi
if [ -z "${DZ_DEVICE_IP:-}" ]; then
    echo "DZ_DEVICE_IP is not set"
    exit 1
fi

# Substitute environment variables in the startup config.
envsubst < /etc/doublezero/agent/startup-config.template > /mnt/flash/startup-config
if [ $? -ne 0 ]; then
    echo "Failed to render startup config"
    exit 1
fi

# Start the device.
exec /sbin/init \
    systemd.setenv=INTFTYPE=eth \
    systemd.setenv=ETBA=4 \
    systemd.setenv=SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT=1 \
    systemd.setenv=CEOS=1 \
    systemd.setenv=EOS_PLATFORM=ceoslab \
    systemd.setenv=container=docker \
    systemd.setenv=MGMT_INTF=eth0 \
    systemd.setenv=MAPETH0=1
