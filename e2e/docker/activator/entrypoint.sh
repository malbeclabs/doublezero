#!/bin/sh

# Check for required environment variables.
if [ -z "${DZ_LEDGER_URL}" ]; then
  echo "DZ_LEDGER_URL is not set"
  exit 1
fi
if [ -z "${DZ_LEDGER_WS}" ]; then
  echo "DZ_LEDGER_WS is not set"
  exit 1
fi
if [ -z "${DZ_PROGRAM_ID}" ]; then
  echo "DZ_PROGRAM_ID is not set"
  exit 1
fi
if [ -z "${DZ_MANAGER_KEYPAIR_PATH}" ]; then
  echo "DZ_MANAGER_KEYPAIR_PATH is not set"
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
doublezero --keypair $DZ_MANAGER_KEYPAIR_PATH config set --url $DZ_LEDGER_URL
doublezero --keypair $DZ_MANAGER_KEYPAIR_PATH config set --ws $DZ_LEDGER_WS
doublezero --keypair $DZ_MANAGER_KEYPAIR_PATH config set --program-id $DZ_PROGRAM_ID

# Start the activator.
doublezero-activator --program-id ${DZ_PROGRAM_ID}
