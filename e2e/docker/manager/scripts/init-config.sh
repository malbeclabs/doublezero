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
if [ -z "${DZ_SERVICEABILITY_PROGRAM_KEYPAIR_PATH}" ]; then
  echo "DZ_SERVICEABILITY_PROGRAM_KEYPAIR_PATH is not set"
  exit 1
fi

# Get the serviceability program ID from the serviceability program keypair.
serviceability_program_id="$(solana address -k $DZ_SERVICEABILITY_PROGRAM_KEYPAIR_PATH)"
echo "==> Serviceability program ID: $serviceability_program_id"
echo

# Initialize doublezero CLI config.
doublezero config set \
  --keypair /root/.config/doublezero/id.json \
  --url $DZ_LEDGER_URL \
  --ws $DZ_LEDGER_WS \
  --program-id $serviceability_program_id
echo "==> Config:"
cat /root/.config/doublezero/cli/config.yml
echo

# Configure the solana CLI.
echo "==> Configuring solana CLI"
solana config set --url $DZ_LEDGER_URL
echo

# Wait for the solana validator to be healthy.
while ! curl -sf -X POST -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"getHealth"}' \
  ${DZ_LEDGER_URL} | grep -q '"result":"ok"'; do
    echo "Waiting for solana validator to be ready..."
    sleep 1
done

# Configure bash completions for doublezero and solana CLIs.
mkdir -p /etc/bash_completion.d
doublezero completion bash > /etc/bash_completion.d/doublezero
solana completion > /etc/bash_completion.d/solana
echo "source /etc/bash_completion.d/doublezero" >> /root/.bashrc
echo "source /etc/bash_completion.d/solana" >> /root/.bashrc

# Done.
echo "==> Config initialized"
