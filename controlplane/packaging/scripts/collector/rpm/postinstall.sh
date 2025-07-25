#!/bin/bash
set -e

# Create system user for the collector
if ! id -u doublezero-collector >/dev/null 2>&1; then
    useradd --system --home-dir /var/lib/doublezero-internet-latency-collector --shell /bin/false doublezero-collector
fi

# Create and set ownership of state and output directories
mkdir -p /var/lib/doublezero-internet-latency-collector/state
mkdir -p /var/lib/doublezero-internet-latency-collector/output
chown -R doublezero-collector:doublezero-collector /var/lib/doublezero-internet-latency-collector

# Ensure proper permissions on the environment file
if [ -f /etc/doublezero-internet-latency-collector/doublezero-internet-latency-collector.env ]; then
    chown root:doublezero-collector /etc/doublezero-internet-latency-collector/doublezero-internet-latency-collector.env
    chmod 0640 /etc/doublezero-internet-latency-collector/doublezero-internet-latency-collector.env
fi

# Reload systemd to pick up the new service
systemctl daemon-reload

# Enable the service but don't start it (user needs to configure API tokens first)
systemctl enable doublezero-internet-latency-collector.service

echo "DoubleZero Internet Latency Collector has been installed."
echo ""
echo "Before starting the service, you need to configure API tokens:"
echo "  1. Edit /etc/doublezero-internet-latency-collector/doublezero-internet-latency-collector.env"
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