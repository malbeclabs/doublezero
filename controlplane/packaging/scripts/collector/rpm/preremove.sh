#!/bin/bash
set -e

# Only run on uninstall, not upgrade
if [ $1 -eq 0 ]; then
    # Stop and disable the service if it's running
    if systemctl is-active --quiet doublezero-internet-latency-collector.service; then
        systemctl stop doublezero-internet-latency-collector.service
    fi

    if systemctl is-enabled --quiet doublezero-internet-latency-collector.service; then
        systemctl disable doublezero-internet-latency-collector.service
    fi
fi