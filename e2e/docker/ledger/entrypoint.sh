#!/usr/bin/env bash
set -euo pipefail

# Build additional flags for solana-test-validator from environment variables.
extra_args=""

ACCOUNTS_DIR="/tmp/fork-accounts"

# Support forking program accounts from a remote cluster.
# This fetches all accounts owned by each program via getProgramAccounts RPC,
# writes them as JSON files, and loads them via --account-dir. The program binary
# is deployed separately via --upgradeable-program with a custom upgrade authority.
#
# CLONE_FROM_URL: the RPC URL to fetch from (e.g., mainnet-beta)
# CLONE_PROGRAM_IDS: comma-separated list of program IDs to fetch accounts for
if [ -n "${CLONE_FROM_URL:-}" ] && [ -n "${CLONE_PROGRAM_IDS:-}" ]; then
  fork-accounts fetch "${CLONE_FROM_URL}" "${CLONE_PROGRAM_IDS}" "${ACCOUNTS_DIR}"
  extra_args="${extra_args} --account-dir ${ACCOUNTS_DIR}"
fi

# Patch the GlobalState account to add a pubkey to the foundation_allowlist.
# This allows a test manager to execute write operations against cloned mainnet state.
#
# PATCH_GLOBALSTATE_AUTHORITY: base58 pubkey to add to the foundation_allowlist
if [ -n "${PATCH_GLOBALSTATE_AUTHORITY:-}" ] && [ -d "${ACCOUNTS_DIR}" ]; then
  fork-accounts patch-globalstate "${ACCOUNTS_DIR}" "${PATCH_GLOBALSTATE_AUTHORITY}"
fi

# Support deploying upgraded programs at startup with a specific upgrade authority.
# UPGRADE_PROGRAM_ID: program ID to upgrade
# UPGRADE_PROGRAM_SO: path to the .so file inside the container
# UPGRADE_AUTHORITY: pubkey of the upgrade authority
if [ -n "${UPGRADE_PROGRAM_ID:-}" ] && [ -n "${UPGRADE_PROGRAM_SO:-}" ] && [ -n "${UPGRADE_AUTHORITY:-}" ]; then
  extra_args="${extra_args} --upgradeable-program ${UPGRADE_PROGRAM_ID} ${UPGRADE_PROGRAM_SO} ${UPGRADE_AUTHORITY}"
  echo "==> Deploying upgraded program ${UPGRADE_PROGRAM_ID} with authority ${UPGRADE_AUTHORITY}"
fi

# Start the solana validator with some noisy output filtered out.
# Configuration and data available at /test-ledger
# Validator logging available at /test-ledger/validator.log
script -q -c "solana-test-validator ${extra_args} 2>&1" /dev/null | grep --line-buffered -v "Processed Slot: "
