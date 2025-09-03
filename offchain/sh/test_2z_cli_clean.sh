#!/bin/bash

set -eu

CLI_BIN=target/debug/2z

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
solana airdrop -u l 1 -k service_key_1.json
echo

echo "solana-keygen new --silent --no-bip39-passphrase -o service_key_2.json"
solana-keygen new --silent --no-bip39-passphrase -o service_key_2.json
solana airdrop -u l 1 -k service_key_2.json
echo

### Admin commands.

$CLI_BIN admin -h
echo

### Passport admin commands.

echo "2z admin passport -h"
$CLI_BIN admin passport -h
echo

echo "2z admin passport initialize -u l -v"
$CLI_BIN admin passport initialize -u l -v
echo

### Set admin to bogus address.
echo "2z admin passport set-admin -u l -v $DUMMY_KEY"
$CLI_BIN admin passport set-admin \
    -u l \
    -v \
    $DUMMY_KEY
echo

### Set admin to upgrade authority.
echo "2z admin passport set-admin -u l -v --fee-payer another_payer.json $(solana address)"
$CLI_BIN admin passport set-admin \
    -u l \
    -v \
    --fee-payer another_payer.json \
    $(solana address)
echo

echo "2z admin passport configure -h"
$CLI_BIN admin passport configure -h
echo

echo "2z admin passport configure -u l -v --pause" \
     "--sentinel $DUMMY_KEY" \
     "--access-request-deposit 1000000000" \
     "--access-fee 100000"
$CLI_BIN admin passport configure -u l \
    -v \
    --pause \
    --sentinel $DUMMY_KEY \
    --access-request-deposit 1000000000 \
    --access-fee 100000
echo

echo "2z admin passport configure -u l -v --unpause"
$CLI_BIN admin passport configure -u l -v --unpause
echo

### Request Solana validator access.

echo "2z passport request-solana-validator-access -h"
$CLI_BIN passport request-solana-validator-access -h
echo

# Generate the signature using solana sign-offchain-message
VALIDATOR_KEYPAIR=test-ledger/validator-keypair.json
NODE_ID=$(solana address -k $VALIDATOR_KEYPAIR)
MESSAGE="service_key=$DUMMY_KEY"
SIGNATURE=$(solana sign-offchain-message -k $VALIDATOR_KEYPAIR service_key=$DUMMY_KEY)

echo "2z passport request-solana-validator-access -u l -v --node-id $NODE_ID --signature $SIGNATURE $DUMMY_KEY"
$CLI_BIN passport request-solana-validator-access \
    -u l \
    -v \
    --node-id $NODE_ID \
    --signature $SIGNATURE \
    $DUMMY_KEY
echo

### Revenue distribution admin commands.

echo "2z admin revenue-distribution -h"
$CLI_BIN admin revenue-distribution -h
echo

echo "2z admin revenue-distribution initialize -u l -v"
$CLI_BIN admin revenue-distribution initialize -u l -v
echo

### Set admin to bogus address.
echo "2z admin revenue-distribution set-admin -u l -v $DUMMY_KEY"
$CLI_BIN admin revenue-distribution set-admin \
    -u l \
    -v \
    $DUMMY_KEY
echo

### Set admin to upgrade authority.
echo "2z admin revenue-distribution set-admin -u l -v --fee-payer another_payer.json $(solana address)"
$CLI_BIN admin revenue-distribution set-admin \
    -u l \
    -v \
    --fee-payer another_payer.json \
    $(solana address)
echo

echo "2z admin revenue-distribution configure -h"
$CLI_BIN admin revenue-distribution configure -h
echo

echo "2z admin revenue-distribution configure -u l -v --pause" \
     "--payments-accountant $DUMMY_KEY --rewards-accountant $DUMMY_KEY" \
     "--contributor-manager $DUMMY_KEY --sentinel $DUMMY_KEY" \
     "--sol-2z-swap-program $DUMMY_KEY --calculation-grace-period-seconds 3600" \
     "--prepaid-connection-termination-relay-lamports 100000" \
     "--solana-validator-base-block-rewards-fee-pct 1.23" \
     "--solana-validator-priority-block-rewards-fee-pct 45.67" \
     "--solana-validator-inflation-rewards-fee-pct 0.89 " \
     "--solana-validator-jito-tips-fee-pct 100" \
     "--solana-validator-fixed-sol-fee-amount 100000000" \
     "--community-burn-rate-limit 50.0 --epochs-to-increasing-community-burn-rate 100" \
     "--epochs-to-community-burn-rate-limit 200 --initial-community-burn-rate 10.0"
$CLI_BIN admin revenue-distribution configure \
    -u l \
    -v \
    --pause \
    --payments-accountant $DUMMY_KEY \
    --rewards-accountant $DUMMY_KEY \
    --contributor-manager $(solana address) \
    --sentinel $DUMMY_KEY \
    --sol-2z-swap-program $DUMMY_KEY \
    --calculation-grace-period-seconds 3600 \
    --prepaid-connection-termination-relay-lamports 100000 \
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

echo "2z admin revenue-distribution configure -u l -v --unpause"
$CLI_BIN admin revenue-distribution configure -u l -v --unpause
echo

echo "2z admin revenue-distribution set-rewards-manager -h"
$CLI_BIN admin revenue-distribution set-rewards-manager -h
echo

echo "2z admin revenue-distribution set-rewards-manager -u l -v " \
     "--rewards-manager $(solana address -k rewards_manager.json) " \
     "--initialize-contributor-rewards " \
     "$(solana address -k service_key_1.json) " \
     "$(solana address -k another_payer.json)"
$CLI_BIN admin revenue-distribution set-rewards-manager \
    -u l \
    -v \
    --initialize-contributor-rewards \
    $(solana address -k service_key_1.json) \
    $(solana address -k another_payer.json)
echo

echo "2z admin revenue-distribution set-rewards-manager -u l -v " \
     "$(solana address -k service_key_1.json) " \
     "$(solana address -k rewards_manager.json)"
$CLI_BIN admin revenue-distribution set-rewards-manager \
    -u l \
    -v \
    $(solana address -k service_key_1.json) \
    $(solana address -k rewards_manager.json)
echo

### Revenue distribution commands.

echo "2z revenue-distribution -h"
$CLI_BIN revenue-distribution -h
echo

echo "2z revenue-distribution initialize-contributor-rewards -h"
$CLI_BIN revenue-distribution initialize-contributor-rewards -h
echo

echo "2z revenue-distribution initialize-contributor-rewards -u l -v $(solana address -k service_key_2.json)"
$CLI_BIN revenue-distribution initialize-contributor-rewards \
    -u l \
    -v \
    $(solana address -k service_key_2.json)
echo

echo "2z admin revenue-distribution set-rewards-manager -u l -v " \
     "$(solana address -k service_key_2.json) " \
     "$(solana address -k rewards_manager.json)"
$CLI_BIN admin revenue-distribution set-rewards-manager \
    -u l \
    -v \
    $(solana address -k service_key_2.json) \
    $(solana address -k rewards_manager.json)
echo

### ATA commands.

echo "2z ata -h"
$CLI_BIN ata -h
echo

### Contributor commands.

echo "2z contributor -h"
$CLI_BIN contributor -h
echo

### Prepaid commands.

echo "2z prepaid -h"
$CLI_BIN prepaid -h
echo

### Validator commands.

echo "2z validator -h"
$CLI_BIN validator -h
echo

### Clean up.

echo "rm dummy.json another_payer.json rewards_manager.json " \
     "service_key_1.json service_key_2.json validator_node_id.json"
rm \
    dummy.json \
    another_payer.json \
    rewards_manager.json \
    service_key_1.json \
    service_key_2.json
