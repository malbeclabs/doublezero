#!/bin/bash
set -e

# Reload systemd after removing the service file
systemctl daemon-reload

# On purge, remove the user and directories
if [ "$1" = "purge" ]; then
    # Remove the system user
    if id -u dz-internet-latency >/dev/null 2>&1; then
        userdel dz-internet-latency
    fi

    # Remove state and output directories
    if [ -d /var/lib/doublezero-internet-latency-collector ]; then
        rm -rf /var/lib/doublezero-internet-latency-collector
    fi
fi
