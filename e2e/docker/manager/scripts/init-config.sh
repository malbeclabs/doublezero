#!/bin/sh

set -e

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

# Initialize doublezero CLI config.
doublezero config set \
  --keypair $DZ_MANAGER_KEYPAIR_PATH \
  --url $DZ_LEDGER_URL \
  --ws $DZ_LEDGER_WS \
  --program-id $DZ_PROGRAM_ID
echo "==> Config:"
cat /root/.config/doublezero/cli/config.yml
echo

# Configure the solana CLI.
echo "==> Configuring solana CLI"
solana config set --url $DZ_LEDGER_URL
echo

# Copy the manager keypair to the default config path for doublezero and solana.
echo "==> Copying manager keypair to doublezero and solana default config paths"
cp $DZ_MANAGER_KEYPAIR_PATH /root/.config/doublezero/id.json
cp $DZ_MANAGER_KEYPAIR_PATH /root/.config/solana/id.json
echo

# Done.
echo "==> Config initialized"
