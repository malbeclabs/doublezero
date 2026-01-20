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

# start device-health-oracle
device-health-oracle -ledger-rpc-url ${DZ_LEDGER_URL} -serviceability-program-id ${DZ_SERVICEABILITY_PROGRAM_ID} -telemetry-program-id ${DZ_TELEMETRY_PROGRAM_ID} -metrics-addr 0.0.0.0:8080 -interval 1m
