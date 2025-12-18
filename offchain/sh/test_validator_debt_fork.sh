#!/bin/bash

MAINNET_BETA_DEBT_ACCOUNTANT_KEY=acLisxTpNkoctPZoqssyo58pcdnHzJyRFhod7Wxkz5a

set -eu

# Wait for Solana fork to start. Only try for 60 seconds.
for i in {1..60}; do
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

CLI_BIN=target/debug/doublezero-solana-validator-debt

$CLI_BIN -h
echo

### Initialize.

echo "doublezero-solana initialize-distribution -h"
$CLI_BIN initialize-distribution -h
echo

echo "doublezero-solana initialize-distribution -v -ul --dz-env mainnet-beta --bypass-dz-epoch-check --record-debt-accountant ${MAINNET_BETA_DEBT_ACCOUNTANT_KEY}"
$CLI_BIN initialize-distribution \
    -v \
    -ul \
    --dz-env mainnet-beta \
    --bypass-dz-epoch-check \
    --record-debt-accountant $MAINNET_BETA_DEBT_ACCOUNTANT_KEY
echo

### In --god-mode, the time to wait for a new initialized distribution is one
### minute.
echo "sleep 60"
sleep 60

echo "doublezero-solana initialize-distribution -v -ul --dz-env mainnet-beta --bypass-dz-epoch-check --record-debt-accountant ${MAINNET_BETA_DEBT_ACCOUNTANT_KEY}"
$CLI_BIN initialize-distribution \
    -v \
    -ul \
    --dz-env mainnet-beta \
    --bypass-dz-epoch-check \
    --record-debt-accountant $MAINNET_BETA_DEBT_ACCOUNTANT_KEY
echo