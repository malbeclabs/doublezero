#!/bin/bash

set -eu

# Wait for the Solana fork the caller started in the background.
# shellcheck source=lib/wait_for_fork.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/wait_for_fork.sh"
wait_for_fork

CLI_BIN=target/debug/doublezero-solana

$CLI_BIN -h
echo

echo "solana-keygen new --silent --no-bip39-passphrase -o dummy.json"
solana-keygen new --silent --no-bip39-passphrase -o dummy.json
solana airdrop -ul 1 -k dummy.json
echo

DUMMY_KEY=$(solana address -k dummy.json)

### Establish rewards manager.
echo "solana-keygen new --silent --no-bip39-passphrase -o rewards_manager.json"
solana-keygen new --silent --no-bip39-passphrase -o rewards_manager.json
solana airdrop -ul 1 -k rewards_manager.json
echo

### Establish service keys.

echo "solana-keygen new --silent --no-bip39-passphrase -o service_key_1.json"
solana-keygen new --silent --no-bip39-passphrase -o service_key_1.json
echo

### Passport commands.

echo "doublezero-solana passport -h"
$CLI_BIN passport -h
echo

echo "doublezero-solana passport fetch -h"
$CLI_BIN passport fetch -h
echo

echo "doublezero-solana passport fetch -ul --config"
$CLI_BIN passport fetch -ul --config
echo

echo "doublezero-solana passport request-validator-access -h"
$CLI_BIN passport request-validator-access -h
echo

# Generate the signature using solana sign-offchain-message
VALIDATOR_KEYPAIR=test-ledger/validator-keypair.json
NODE_ID=$(solana address -k $VALIDATOR_KEYPAIR)
MESSAGE="service_key=$DUMMY_KEY"
SIGNATURE=$(solana sign-offchain-message -k $VALIDATOR_KEYPAIR service_key=$DUMMY_KEY)

echo "doublezero-solana passport request-validator-access -ul -v --primary-validator-id $NODE_ID --signature $SIGNATURE --doublezero-address $DUMMY_KEY --leader-schedule-epochs 1"
$CLI_BIN passport request-validator-access \
    -ul \
    -v \
    --primary-validator-id $NODE_ID \
    --signature $SIGNATURE \
    --doublezero-address $DUMMY_KEY \
    --leader-schedule-epochs 1
echo

echo "doublezero-solana passport fetch -ul --access-request $DUMMY_KEY"
$CLI_BIN passport fetch -ul --access-request $DUMMY_KEY
echo

### Revenue distribution commands.

echo "doublezero-solana revenue-distribution -h"
$CLI_BIN revenue-distribution -h
echo

echo "doublezero-solana revenue-distribution fetch -h"
$CLI_BIN revenue-distribution fetch -h
echo

echo "doublezero-solana -ul revenue-distribution fetch config"
$CLI_BIN -ul revenue-distribution fetch config
echo

echo "doublezero-solana -ul revenue-distribution fetch validator-deposits"
$CLI_BIN -ul revenue-distribution fetch validator-deposits
echo

### Backwards compatibility: the legacy per-verb (trailing) flag form must keep
### working. These mirror the global-flag invocations above but pass --url/-u and
### the hidden --dz-env AFTER the subcommand, the way pre-RFC-20 scripts did. The
### output must match the equivalent global-flag invocation.

echo "[back-compat] doublezero-solana revenue-distribution fetch config -ul"
$CLI_BIN revenue-distribution fetch config -ul
echo

echo "[back-compat] doublezero-solana revenue-distribution fetch validator-deposits -ul"
$CLI_BIN revenue-distribution fetch validator-deposits -ul
echo

echo "[back-compat] doublezero-solana revenue-distribution fetch distribution -ul --dz-env mainnet-beta"
$CLI_BIN revenue-distribution fetch distribution -ul --dz-env mainnet-beta
echo

echo "doublezero-solana revenue-distribution contributor-rewards -h"
$CLI_BIN revenue-distribution contributor-rewards -h
echo

echo "doublezero-solana -ul revenue-distribution contributor-rewards --initialize -v $(solana address -k service_key_1.json)"
$CLI_BIN -ul revenue-distribution contributor-rewards \
    --initialize \
    -v \
    $(solana address -k service_key_1.json)
echo

echo "doublezero-solana -ul revenue-distribution validator-deposit --fund 4.2069 -v --node-id $DUMMY_KEY"
$CLI_BIN -ul revenue-distribution validator-deposit \
    --fund 4.2069 \
    -v \
    --node-id $DUMMY_KEY
echo

echo "doublezero-solana -ul revenue-distribution validator-deposit --fund 69.420 -v --node-id $DUMMY_KEY"
$CLI_BIN -ul revenue-distribution validator-deposit \
    --fund 69.420 \
    -v \
    --node-id $DUMMY_KEY
echo

echo "doublezero-solana -ul revenue-distribution fetch validator-deposits --node-id $DUMMY_KEY"
$CLI_BIN -ul revenue-distribution fetch validator-deposits --node-id $DUMMY_KEY
echo

echo "doublezero-solana -ul revenue-distribution fetch validator-deposits --node-id $DUMMY_KEY --balance-only"
$CLI_BIN -ul revenue-distribution fetch validator-deposits --node-id $DUMMY_KEY --balance-only
echo

echo "doublezero-solana -ul revenue-distribution fetch validator-deposits"
$CLI_BIN -ul revenue-distribution fetch validator-deposits
echo

echo "doublezero-solana -ul revenue-distribution fetch distribution"
$CLI_BIN -ul revenue-distribution fetch distribution
echo

echo "doublezero-solana -um revenue-distribution fetch distribution"
$CLI_BIN -um revenue-distribution fetch distribution
echo

echo "doublezero-solana -um revenue-distribution fetch distribution --view summary"
$CLI_BIN -um revenue-distribution fetch distribution --view summary
echo

echo "doublezero-solana -ul revenue-distribution validator-deposit --withdraw-excess-balance -v --node-id $DUMMY_KEY"
$CLI_BIN -ul revenue-distribution validator-deposit \
    --withdraw-excess-balance \
    -v \
    --node-id $DUMMY_KEY
echo

### Clean up.

echo "rm dummy.json rewards_manager.json service_key_1.json"
rm \
    dummy.json \
    rewards_manager.json \
    service_key_1.json
