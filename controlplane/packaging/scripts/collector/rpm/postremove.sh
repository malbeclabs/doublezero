#!/bin/bash
set -e

# Reload systemd after removing the service file
systemctl daemon-reload

# Only run on complete uninstall, not upgrade
if [ $1 -eq 0 ]; then
    # Remove the system user
    if id -u doublezero-internet-latency-collector >/dev/null 2>&1; then
        userdel dz-internet-latency
    fi

    # Remove state and output directories
    if [ -d /var/lib/doublezero-internet-latency-collector ]; then
        rm -rf /var/lib/doublezero-internet-latency-collector
    fi
fi
