#!/bin/bash
set -e

# Create system user for the collector
if ! id -u dz-internet-latency >/dev/null 2>&1; then
    useradd --system --home-dir /var/lib/doublezero-internet-latency-collector --shell /bin/false dz-internet-latency
fi

# Create and set ownership of state and output directories
mkdir -p /var/lib/doublezero-internet-latency-collector/state
mkdir -p /var/lib/doublezero-internet-latency-collector/output
chown -R dz-internet-latency:dz-internet-latency /var/lib/doublezero-internet-latency-collector

# Reload systemd to pick up the new service
systemctl daemon-reexec
systemctl daemon-reload

# If config.env file exists, enable + restart
if [ -f /etc/doublezero-internet-latency-collector/config.env ]; then
    deb-systemd-helper unmask 'doublezero-internet-latency-collector.service' >/dev/null || true
    deb-systemd-helper enable 'doublezero-internet-latency-collector.service' >/dev/null || true

    if [ -d /run/systemd/system ]; then
        systemctl restart doublezero-internet-latency-collector.service || true
    fi
else
    # Fresh install, no config: don't start yet, but still enable it
    deb-systemd-helper enable 'doublezero-internet-latency-collector.service' >/dev/null || true
    echo "Config file missing. Please edit /etc/doublezero-internet-latency-collector/config.env..."

    echo "DoubleZero Internet Latency Collector has been installed."
    echo ""
    echo "Before starting the service, you need to configure API tokens:"
    echo "  1. Edit /etc/doublezero-internet-latency-collector/config.env"
    echo "  2. Set your WHERESITUP_API_TOKEN and RIPE_ATLAS_API_KEY values"
    echo ""
    echo "Then start the service with:"
    echo "  sudo systemctl start doublezero-internet-latency-collector"
    echo ""
    echo "To check the service status:"
    echo "  sudo systemctl status doublezero-internet-latency-collector"
    echo ""
    echo "Logs can be viewed with:"
    echo "  sudo journalctl -u doublezero-internet-latency-collector -f"
fi
