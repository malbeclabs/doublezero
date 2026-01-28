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

# Initialize config for talking to the smart contract on the DZ ledger (solana).
doublezero config set --url $DZ_LEDGER_URL
doublezero config set --ws $DZ_LEDGER_WS
doublezero config set --program-id $DZ_SERVICEABILITY_PROGRAM_ID

# Build activator command with optional flags.
ACTIVATOR_CMD="doublezero-activator --program-id ${DZ_SERVICEABILITY_PROGRAM_ID} --rpc ${DZ_LEDGER_URL} --ws ${DZ_LEDGER_WS} --keypair /root/.config/doublezero/id.json --onchain-allocation"

# Start the activator.
exec ${ACTIVATOR_CMD}
