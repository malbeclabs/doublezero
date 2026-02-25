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
if [ -z "${DZ_TELEMETRY_PROGRAM_ID}" ]; then
  echo "DZ_TELEMETRY_PROGRAM_ID is not set"
  exit 1
fi
if [ -z "${DZ_SIGNER_KEYPAIR}" ]; then
  echo "DZ_SIGNER_KEYPAIR is not set"
  exit 1
fi

# Start Alloy in background if ALLOY_PROMETHEUS_URL is set
if [ -n "${ALLOY_PROMETHEUS_URL:-}" ]; then
  echo "Starting Alloy..."
  alloy run /etc/alloy/config.alloy &
fi

# start device-health-oracle
device-health-oracle -ledger-rpc-url ${DZ_LEDGER_URL} -serviceability-program-id ${DZ_SERVICEABILITY_PROGRAM_ID} -telemetry-program-id ${DZ_TELEMETRY_PROGRAM_ID} -signer-keypair ${DZ_SIGNER_KEYPAIR} -metrics-addr ":2112" -interval ${DZ_INTERVAL:-1m}
