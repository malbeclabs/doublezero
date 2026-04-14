#!/usr/bin/env bash
set -euo pipefail

# Check for required environment variables.
if [ -z "${DZ_LEDGER_URL}" ]; then
  echo "DZ_LEDGER_URL is not set"
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

# Start Alloy in background if ALLOY_PROMETHEUS_URL is set
if [ -n "${ALLOY_PROMETHEUS_URL:-}" ]; then
  echo "Starting Alloy..."
  alloy run /etc/alloy/config.alloy &
fi

# Start the controller.
doublezero-controller start -listen-addr 0.0.0.0 -listen-port 7000 -program-id ${DZ_SERVICEABILITY_PROGRAM_ID} -solana-rpc-endpoint ${DZ_LEDGER_URL} -device-local-asn 65342 -no-hardware
