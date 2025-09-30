#!/bin/bash

set -eu

CLI_BIN=target/debug/doublezero-solana

$CLI_BIN -h
echo

echo "solana-keygen new --silent --no-bip39-passphrase -o dummy.json"
solana-keygen new --silent --no-bip39-passphrase -o dummy.json
solana airdrop -u l 1 -k dummy.json
echo

DUMMY_KEY=$(solana address -k dummy.json)

### Establish another payer.

echo "solana-keygen new --silent --no-bip39-passphrase -o another_payer.json"
solana-keygen new --silent --no-bip39-passphrase -o another_payer.json
solana airdrop -u l 69 -k another_payer.json
echo

### Establish rewards manager.
echo "solana-keygen new --silent --no-bip39-passphrase -o rewards_manager.json"
solana-keygen new --silent --no-bip39-passphrase -o rewards_manager.json
solana airdrop -u l 1 -k rewards_manager.json
echo

### Establish service keys.

echo "solana-keygen new --silent --no-bip39-passphrase -o service_key_1.json"
solana-keygen new --silent --no-bip39-passphrase -o service_key_1.json
echo

### Set up programs with admin CLI.

echo "doublezero-passport-admin initialize -u l -v"
doublezero-passport-admin initialize -u l -v
echo

echo "doublezero-passport-admin configure -u l -v --unpause" \
     "--sentinel $DUMMY_KEY" \
     "--access-request-deposit 1000000000" \
     "--access-fee 100000"
doublezero-passport-admin configure -u l \
    -v \
    --unpause \
    --sentinel $DUMMY_KEY \
    --access-request-deposit 1000000000 \
    --access-fee 100000
echo

echo "doublezero-revenue-distribution-admin initialize -u l -v"
doublezero-revenue-distribution-admin initialize -u l -v
echo

echo "doublezero-revenue-distribution-admin configure -u l -v --unpause" \
     "--contributor-manager $(solana address)" \
     "--calculation-grace-period-minutes 60" \
     "--solana-validator-base-block-rewards-fee-pct 1.23" \
     "--solana-validator-priority-block-rewards-fee-pct 45.67" \
     "--solana-validator-inflation-rewards-fee-pct 0.89 " \
     "--solana-validator-jito-tips-fee-pct 100" \
     "--solana-validator-fixed-sol-fee-amount 100000000" \
     "--community-burn-rate-limit 50.0 --epochs-to-increasing-community-burn-rate 100" \
     "--epochs-to-community-burn-rate-limit 200 --initial-community-burn-rate 10.0"
doublezero-revenue-distribution-admin configure \
    -u l \
    -v \
    --unpause \
    --contributor-manager $(solana address) \
    --calculation-grace-period-minutes 60 \
    --solana-validator-base-block-rewards-fee-pct 1.23 \
    --solana-validator-priority-block-rewards-fee-pct 45.67 \
    --solana-validator-inflation-rewards-fee-pct 0.89 \
    --solana-validator-jito-tips-fee-pct 100 \
    --solana-validator-fixed-sol-fee-amount 100000000 \
    --community-burn-rate-limit 50.0 \
    --epochs-to-increasing-community-burn-rate 100 \
    --epochs-to-community-burn-rate-limit 200 \
    --initial-community-burn-rate 10.0
echo

### Passport commands.

echo "doublezero-solana passport -h"
$CLI_BIN passport -h
echo

echo "doublezero-solana passport fetch -h"
$CLI_BIN passport fetch -h
echo

echo "doublezero-solana passport fetch -u l --config --access-request $DUMMY_KEY"
$CLI_BIN passport fetch -u l --config --access-request $DUMMY_KEY
echo

echo "doublezero-solana passport request-validator-access -h"
$CLI_BIN passport request-validator-access -h
echo

# Generate the signature using solana sign-offchain-message
VALIDATOR_KEYPAIR=test-ledger/validator-keypair.json
NODE_ID=$(solana address -k $VALIDATOR_KEYPAIR)
MESSAGE="service_key=$DUMMY_KEY"
SIGNATURE=$(solana sign-offchain-message -k $VALIDATOR_KEYPAIR service_key=$DUMMY_KEY)

echo "doublezero-solana passport request-validator-access -u l -v --primary-validator-id $NODE_ID --signature $SIGNATURE --doublezero-address $DUMMY_KEY"
$CLI_BIN passport request-validator-access \
    -u l \
    -v \
    --primary-validator-id $NODE_ID \
    --signature $SIGNATURE \
    --doublezero-address $DUMMY_KEY
echo

echo "doublezero-solana passport fetch -u l --access-request $DUMMY_KEY"
$CLI_BIN passport fetch -u l --access-request $DUMMY_KEY
echo

### Revenue distribution commands.

echo "doublezero-solana revenue-distribution -h"
$CLI_BIN revenue-distribution -h
echo

echo "doublezero-solana revenue-distribution fetch -h"
$CLI_BIN revenue-distribution fetch -h
echo

echo "doublezero-solana revenue-distribution fetch config -u l"
$CLI_BIN revenue-distribution fetch config -u l
echo

echo "doublezero-solana revenue-distribution fetch validator-deposits -u l --node-id $DUMMY_KEY"
$CLI_BIN revenue-distribution fetch validator-deposits -u l --node-id $DUMMY_KEY
echo

echo "doublezero-solana revenue-distribution fetch validator-deposits -u l"
$CLI_BIN revenue-distribution fetch validator-deposits -u l
echo

echo "doublezero-solana revenue-distribution contributor-rewards -h"
$CLI_BIN revenue-distribution contributor-rewards -h
echo

echo "doublezero-solana revenue-distribution contributor-rewards -u l --initialize -v $(solana address -k service_key_1.json)"
$CLI_BIN revenue-distribution contributor-rewards \
    -u l \
    --initialize \
    -v \
    $(solana address -k service_key_1.json)
echo

echo "doublezero-revenue-distribution-admin set-rewards-manager -u l -v " \
     "$(solana address -k service_key_1.json) " \
     "$(solana address -k rewards_manager.json)"
doublezero-revenue-distribution-admin set-rewards-manager \
    -u l \
    -v \
    $(solana address -k service_key_1.json) \
    $(solana address -k rewards_manager.json)
echo

echo "doublezero-solana revenue-distribution validator-deposit --fund 4.2069 -u l -v $DUMMY_KEY"
$CLI_BIN revenue-distribution validator-deposit \
    --fund 4.2069 \
    -u l \
    -v \
    $DUMMY_KEY
echo

echo "doublezero-solana revenue-distribution validator-deposit --fund 69.420 -u l -v $DUMMY_KEY"
$CLI_BIN revenue-distribution validator-deposit \
    --fund 69.420 \
    -u l \
    -v \
    $DUMMY_KEY
echo

echo "doublezero-solana revenue-distribution fetch validator-deposits -u l --node-id $DUMMY_KEY"
$CLI_BIN revenue-distribution fetch validator-deposits -u l --node-id $DUMMY_KEY
echo

echo "doublezero-solana revenue-distribution fetch validator-deposits -u l"
$CLI_BIN revenue-distribution fetch validator-deposits -u l
echo

### ATA commands.

echo "doublezero-solana ata -h"
$CLI_BIN ata -h
echo

### Clean up.

echo "rm dummy.json another_payer.json rewards_manager.json " \
     "service_key_1.json service_key_1.json validator_node_id.json"
rm \
    dummy.json \
    another_payer.json \
    rewards_manager.json \
    service_key_1.json
