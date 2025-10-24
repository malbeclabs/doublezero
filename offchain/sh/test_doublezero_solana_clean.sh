#!/bin/bash

set -eu

# Wait for Solana fork to start. Only try for 60 seconds.
for i in {1..30}; do
    if solana cluster-version -u l > /dev/null 2>&1; then
        echo "Solana fork is ready."
        break
    fi
        sleep 2
done

# If not ready after 60 seconds, bail out.
if ! solana cluster-version -u l > /dev/null 2>&1; then
    echo "Solana fork did not start within 60 seconds." >&2
    exit 1
fi

CLI_BIN=target/debug/doublezero-solana

$CLI_BIN -h
echo

echo "solana-keygen new --silent --no-bip39-passphrase -o dummy.json"
solana-keygen new --silent --no-bip39-passphrase -o dummy.json
solana airdrop -ul 1 -k dummy.json
echo

DUMMY_KEY=$(solana address -k dummy.json)

### Establish another payer.

echo "solana-keygen new --silent --no-bip39-passphrase -o another_payer.json"
solana-keygen new --silent --no-bip39-passphrase -o another_payer.json
solana airdrop -ul 69 -k another_payer.json
echo

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

echo "doublezero-solana passport request-validator-access -ul -v --primary-validator-id $NODE_ID --signature $SIGNATURE --doublezero-address $DUMMY_KEY"
$CLI_BIN passport request-validator-access \
    -ul \
    -v \
    --primary-validator-id $NODE_ID \
    --signature $SIGNATURE \
    --doublezero-address $DUMMY_KEY
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

echo "doublezero-solana revenue-distribution fetch config -ul"
$CLI_BIN revenue-distribution fetch config -ul
echo

echo "doublezero-solana revenue-distribution fetch validator-deposits -ul"
$CLI_BIN revenue-distribution fetch validator-deposits -ul
echo

echo "doublezero-solana revenue-distribution contributor-rewards -h"
$CLI_BIN revenue-distribution contributor-rewards -h
echo

echo "doublezero-solana revenue-distribution contributor-rewards -ul --initialize -v $(solana address -k service_key_1.json)"
$CLI_BIN revenue-distribution contributor-rewards \
    -ul \
    --initialize \
    -v \
    $(solana address -k service_key_1.json)
echo

echo "doublezero-solana revenue-distribution validator-deposit --fund 4.2069 -ul -v --node-id $DUMMY_KEY"
$CLI_BIN revenue-distribution validator-deposit \
    --fund 4.2069 \
    -ul \
    -v \
    --node-id $DUMMY_KEY
echo

echo "doublezero-solana revenue-distribution validator-deposit --fund 69.420 -ul -v --node-id $DUMMY_KEY"
$CLI_BIN revenue-distribution validator-deposit \
    --fund 69.420 \
    -ul \
    -v \
    --node-id $DUMMY_KEY
echo

echo "doublezero-solana revenue-distribution fetch validator-deposits -ul --node-id $DUMMY_KEY"
$CLI_BIN revenue-distribution fetch validator-deposits -ul --node-id $DUMMY_KEY
echo

echo "doublezero-solana revenue-distribution fetch validator-deposits -ul --node-id $DUMMY_KEY --balance-only"
$CLI_BIN revenue-distribution fetch validator-deposits -ul --node-id $DUMMY_KEY --balance-only
echo

echo "doublezero-solana revenue-distribution fetch validator-deposits -ul"
$CLI_BIN revenue-distribution fetch validator-deposits -ul
echo

echo "doublezero-solana revenue-distribution fetch distribution -ul"
$CLI_BIN revenue-distribution fetch distribution -ul
echo

echo "doublezero-solana revenue-distribution fetch distribution -ul --dz-epoch 1"
$CLI_BIN revenue-distribution fetch distribution -ul --dz-epoch 1
echo

echo "doublezero-solana revenue-distribution fetch distribution -ul -e 1"
$CLI_BIN revenue-distribution fetch distribution -ul -e 1
echo

### Clean up.

echo "rm dummy.json another_payer.json rewards_manager.json " \
     "service_key_1.json service_key_1.json validator_node_id.json"
rm \
    dummy.json \
    another_payer.json \
    rewards_manager.json \
    service_key_1.json
