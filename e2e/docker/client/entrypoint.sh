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

# Start doublezerod.
doublezerod -program-id ${DZ_SERVICEABILITY_PROGRAM_ID} -solana-rpc-endpoint ${DZ_LEDGER_URL} -probe-interval 5 -cache-update-interval 3 -bgp-hold-time 3
