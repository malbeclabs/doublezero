#!/bin/bash

set -eu

CLI_BIN=target/release/2z

$CLI_BIN -h
echo

### Establish another payer.

echo "solana-keygen new --silent --no-bip39-passphrase -o another_payer.json"
solana-keygen new --silent --no-bip39-passphrase -o another_payer.json
solana airdrop -u l 69 -k another_payer.json
echo

DUMMY_KEY=devgM7SXHvoHH6jPXRsjn97gygPUo58XEnc9bqY1jpj

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
echo "2z admin passport set-admin -u l -v devgM7SXHvoHH6jPXRsjn97gygPUo58XEnc9bqY1jpj"
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

echo "2z admin passport configure -u l -v --pause"
$CLI_BIN admin passport configure -u l -v --pause
echo

echo "2z admin passport configure -u l -v --unpause"
$CLI_BIN admin passport configure -u l -v --unpause
echo

### Revenue distribution admin commands.

echo "2z admin revenue-distribution -h"
$CLI_BIN admin revenue-distribution -h
echo

echo "2z admin revenue-distribution initialize -u l -v"
$CLI_BIN admin revenue-distribution initialize -u l -v
echo

### Set admin to bogus address.
echo "2z admin revenue-distribution set-admin -u l -v devgM7SXHvoHH6jPXRsjn97gygPUo58XEnc9bqY1jpj"
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
     "--sol-2z-swap-program $DUMMY_KEY --calculation-grace-period-seconds 3600" \
     "--prepaid-connection-termination-relay-lamports 100000" \
     "--solana-validator-base-block-rewards-fee 1.23" \
     "--solana-validator-priority-block-rewards-fee 45.67" \
     "--solana-validator-inflation-rewards-fee 0.89 --solana-validator-jito-tips-fee 100" \
     "--community-burn-rate-limit 50.0 --epochs-to-increasing-community-burn-rate 100" \
     "--epochs-to-community-burn-rate-limit 200 --initial-community-burn-rate 10.0"
$CLI_BIN admin revenue-distribution configure \
    -u l \
    -v \
    --pause \
    --payments-accountant $DUMMY_KEY \
    --rewards-accountant $DUMMY_KEY \
    --sol-2z-swap-program $DUMMY_KEY \
    --calculation-grace-period-seconds 3600 \
    --prepaid-connection-termination-relay-lamports 100000 \
    --solana-validator-base-block-rewards-fee 1.23 \
    --solana-validator-priority-block-rewards-fee 45.67 \
    --solana-validator-inflation-rewards-fee 0.89 \
    --solana-validator-jito-tips-fee 100 \
    --community-burn-rate-limit 50.0 \
    --epochs-to-increasing-community-burn-rate 100 \
    --epochs-to-community-burn-rate-limit 200 \
    --initial-community-burn-rate 10.0
echo

echo "2z admin revenue-distribution configure -u l -v --unpause"
$CLI_BIN admin revenue-distribution configure -u l -v --unpause
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

echo "rm another_payer.json"
rm another_payer.json
