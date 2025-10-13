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

echo "doublezero-solana revenue-distribution fetch distribution -u l"
$CLI_BIN revenue-distribution fetch distribution -u l
echo

echo "doublezero-solana revenue-distribution fetch distribution -u l --epoch 1"
$CLI_BIN revenue-distribution fetch distribution -u l --epoch 1
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
