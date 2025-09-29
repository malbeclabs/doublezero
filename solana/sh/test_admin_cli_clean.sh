#!/bin/bash

set -eu

PASSPORT_CLI_BIN=target/debug/doublezero-passport-admin
REVENUE_DISTRIBUTION_CLI_BIN=target/debug/doublezero-revenue-distribution-admin

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

### Passport admin commands.

echo "doublezero-passport-admin -h"
$PASSPORT_CLI_BIN -h
echo

echo "doublezero-passport-admin initialize -u l -v"
$PASSPORT_CLI_BIN initialize -u l -v
echo

### Set admin to bogus address.
echo "doublezero-passport-admin set-admin -u l -v devgM7SXHvoHH6jPXRsjn97gygPUo58XEnc9bqY1jpj"
$PASSPORT_CLI_BIN set-admin \
    -u l \
    -v \
    $DUMMY_KEY
echo

### Set admin to upgrade authority.
echo "doublezero-passport-admin set-admin -u l -v --fee-payer another_payer.json $(solana address)"
$PASSPORT_CLI_BIN set-admin \
    -u l \
    -v \
    --fee-payer another_payer.json \
    $(solana address)
echo

echo "doublezero-passport-admin configure -h"
$PASSPORT_CLI_BIN configure -h
echo

echo "doublezero-passport-admin configure -u l -v --pause" \
     "--sentinel $DUMMY_KEY" \
     "--access-request-deposit 1000000000" \
     "--access-fee 100000" \
     "--solana-validator-backup-ids-limit 1"
$PASSPORT_CLI_BIN configure -u l \
    -v \
    --pause \
    --sentinel $DUMMY_KEY \
    --access-request-deposit 1000000000 \
    --access-fee 100000 \
    --solana-validator-backup-ids-limit 1
echo

echo "doublezero-passport-admin configure -u l -v --unpause"
$PASSPORT_CLI_BIN configure -u l -v --unpause
echo

### Revenue distribution admin commands.

echo "doublezero-revenue-distribution-admin -h"
$REVENUE_DISTRIBUTION_CLI_BIN -h
echo

echo "doublezero-revenue-distribution-admin initialize -u l -v"
$REVENUE_DISTRIBUTION_CLI_BIN initialize -u l -v
echo

### Set admin to bogus address.
echo "doublezero-revenue-distribution-admin set-admin -u l -v devgM7SXHvoHH6jPXRsjn97gygPUo58XEnc9bqY1jpj"
$REVENUE_DISTRIBUTION_CLI_BIN set-admin \
    -u l \
    -v \
    $DUMMY_KEY
echo

### Set admin to upgrade authority.
echo "doublezero-revenue-distribution-admin set-admin -u l -v --fee-payer another_payer.json $(solana address)"
$REVENUE_DISTRIBUTION_CLI_BIN set-admin \
    -u l \
    -v \
    --fee-payer another_payer.json \
    $(solana address)
echo

echo "doublezero-revenue-distribution-admin configure -h"
$REVENUE_DISTRIBUTION_CLI_BIN configure -h
echo

echo "doublezero-revenue-distribution-admin configure -u l -v --pause" \
     "--debt-accountant $DUMMY_KEY --rewards-accountant $DUMMY_KEY" \
     "--contributor-manager $DUMMY_KEY" \
     "--sol-2z-swap-program $DUMMY_KEY --calculation-grace-period-seconds 3600" \
     "--solana-validator-base-block-rewards-fee-pct 1.23" \
     "--solana-validator-priority-block-rewards-fee-pct 45.67" \
     "--solana-validator-inflation-rewards-fee-pct 0.89 " \
     "--solana-validator-jito-tips-fee-pct 100" \
     "--solana-validator-fixed-sol-fee-amount 100000000" \
     "--community-burn-rate-limit 50.0 --epochs-to-increasing-community-burn-rate 100" \
     "--epochs-to-community-burn-rate-limit 200 --initial-community-burn-rate 10.0"
$REVENUE_DISTRIBUTION_CLI_BIN configure \
    -u l \
    -v \
    --pause \
    --debt-accountant $DUMMY_KEY \
    --rewards-accountant $DUMMY_KEY \
    --contributor-manager $(solana address) \
    --sol-2z-swap-program $DUMMY_KEY \
    --calculation-grace-period-seconds 3600 \
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

echo "doublezero-revenue-distribution-admin configure -u l -v --unpause"
$REVENUE_DISTRIBUTION_CLI_BIN configure -u l -v --unpause
echo

echo "doublezero-revenue-distribution-admin set-rewards-manager -h"
$REVENUE_DISTRIBUTION_CLI_BIN set-rewards-manager -h
echo

echo "doublezero-revenue-distribution-admin set-rewards-manager -u l -v " \
     "--rewards-manager $(solana address -k rewards_manager.json) " \
     "--initialize-contributor-rewards " \
     "$(solana address -k service_key_1.json) " \
     "$(solana address -k another_payer.json)"
$REVENUE_DISTRIBUTION_CLI_BIN set-rewards-manager \
    -u l \
    -v \
    --initialize-contributor-rewards \
    $(solana address -k service_key_1.json) \
    $(solana address -k another_payer.json)
echo

echo "doublezero-revenue-distribution-admin set-rewards-manager -u l -v " \
     "$(solana address -k service_key_1.json) " \
     "$(solana address -k rewards_manager.json)"
$REVENUE_DISTRIBUTION_CLI_BIN set-rewards-manager \
    -u l \
    -v \
    $(solana address -k service_key_1.json) \
    $(solana address -k rewards_manager.json)
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
