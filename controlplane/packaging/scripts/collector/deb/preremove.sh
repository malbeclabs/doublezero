#!/bin/bash
set -e

# Stop and disable the service if it's running
if systemctl is-active --quiet doublezero-internet-latency-collector.service; then
    systemctl stop doublezero-internet-latency-collector.service
fi

if systemctl is-enabled --quiet doublezero-internet-latency-collector.service; then
    systemctl disable doublezero-internet-latency-collector.service
fi