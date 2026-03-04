#!/usr/bin/env bash
set -euo pipefail

# Check for required environment variables.
if [ -z "${DZ_LEDGER_URL}" ]; then
  echo "DZ_LEDGER_URL is not set"
  exit 1
fi
if [ -z "${DZ_LEDGER_WS}" ]; then
  echo "DZ_LEDGER_WS is not set"
  exit 1
fi
if [ -z "${DZ_SERVICEABILITY_PROGRAM_ID}" ]; then
  echo "DZ_SERVICEABILITY_PROGRAM_ID is not set"
  exit 1
fi

# Wait for the solana validator to be healthy.
while ! curl -sf -X POST -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"getHealth"}' \
  ${DZ_LEDGER_URL} | grep -q '"result":"ok"'; do
    echo "Waiting for solana validator to be ready..."
    sleep 1
done

# Initialize doublezero CLI config.
# NOTE: We assume that /root/.config/doublezero/id.json exists already.
doublezero config set \
  --url $DZ_LEDGER_URL \
  --ws $DZ_LEDGER_WS \
  --program-id $DZ_SERVICEABILITY_PROGRAM_ID
echo "==> Config:"
cat /root/.config/doublezero/cli/config.yml
echo

# Configure the solana CLI.
# NOTE: We assume that /root/.config/solana/id.json exists already.
echo "==> Configuring solana CLI"
solana config set --url $DZ_LEDGER_URL
echo

# Configure bash completions for doublezero and solana CLIs.
mkdir -p /etc/bash_completion.d
doublezero completion bash > /etc/bash_completion.d/doublezero
solana completion > /etc/bash_completion.d/solana
echo "source /etc/bash_completion.d/doublezero" >> /root/.bashrc
echo "source /etc/bash_completion.d/solana" >> /root/.bashrc

# Create path for socket file.
mkdir -p /var/run/doublezerod

# Delete the socket file if it exists at this point.
rm -f /var/run/doublezerod/doublezerod.sock

# Create state file directory.
mkdir -p /var/lib/doublezerod

# Workaround for Arista cEOS GRE/MPLS encapsulation bug
# ----------------------------------------------------
# Arista containers sometimes rewrite outbound GRE packets with the wrong
# Protocol Type (0x8847, MPLS) instead of the correct 0x0800 (IPv4).
# This causes the receiving Linux host to reject GRE decapsulation, and the
# inner ICMP traffic (e.g., client1 <-> client2) never reaches the tunnel.
#
# The tc filter below patches inbound GRE packets on the client side,
# rewriting the GRE Protocol Type field back to 0x0800 before they reach
# the kernel’s GRE handler. This effectively “undoes” the Arista bug so the
# packets are treated as normal IPv4-in-GRE traffic.
#
# Safe to remove once cEOS (or upstream DZD routing) stops emitting
# GREv0/MPLS frames.
for dev in $(ip -o link show | awk -F': ' '/^ *[0-9]+: eth[0-9]+/ {print $2}' | cut -d@ -f1); do
  tc qdisc add dev "$dev" clsact 2>/dev/null || true
  tc filter add dev "$dev" ingress protocol ip prio 10 u32 \
    match ip protocol 47 0xff \
    action pedit munge offset 22 u16 set 0x0800 pipe action pass 2>/dev/null || true
done

# Start QA agent if enabled (for local QA testing).
if [ "${DZ_QAAGENT_ENABLE:-}" = "true" ]; then
  QAAGENT_ADDR="${DZ_QAAGENT_ADDR:-0.0.0.0:7009}"
  echo "==> Starting QA Agent on ${QAAGENT_ADDR}"
  doublezero-qaagent -server-addr "${QAAGENT_ADDR}" &
  QAAGENT_PID=$!
  sleep 1
  if ! kill -0 "$QAAGENT_PID" 2>/dev/null; then
    echo "ERROR: QA Agent failed to start"
    exit 1
  fi
fi

# Start doublezerod.
doublezerod --env localnet -program-id ${DZ_SERVICEABILITY_PROGRAM_ID} -solana-rpc-endpoint ${DZ_LEDGER_URL} -probe-interval 5 -cache-update-interval 3 -metrics-enable -metrics-addr 0.0.0.0:8080 ${DZ_CLIENT_EXTRA_ARGS}
