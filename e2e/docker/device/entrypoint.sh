#!/usr/bin/env bash
set -euo pipefail

# Wait for startup-config to exist.
config_path="/etc/doublezero/agent/startup-config"
while [ ! -f "$config_path" ]; do
    echo "Waiting for $config_path to exist"
    sleep 1
done

# Copy the startup config to the flash partition.
cp "$config_path" /mnt/flash/startup-config

echo "==> Startup config ready"

# Start the device.
echo "==> Starting device services"
exec /sbin/init \
    systemd.setenv=INTFTYPE=eth \
    systemd.setenv=ETBA=4 \
    systemd.setenv=SKIP_ZEROTOUCH_BARRIER_IN_SYSDBINIT=1 \
    systemd.setenv=CEOS=1 \
    systemd.setenv=EOS_PLATFORM=ceoslab \
    systemd.setenv=container=docker \
    systemd.setenv=MGMT_INTF=eth0 \
    systemd.setenv=MAPETH0=1
