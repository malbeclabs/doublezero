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

### Set up environment.

ADMIN_CLI_BIN=target/debug/doublezero-revenue-distribution-admin
CLI_BIN=target/debug/doublezero-solana-validator-debt

echo "doublezero-revenue-distribution-admin fetch-current-epoch -ul"
CURRENT_EPOCH=$($ADMIN_CLI_BIN fetch-current-epoch -ul)
echo $CURRENT_EPOCH

### Activate Solana validator debt write-off feature after the next epoch.
SOLANA_VALIDATOR_DEBT_WRITE_OFF_ACTIVATION_EPOCH=$((CURRENT_EPOCH + 1))

echo "doublezero-revenue-distribution-admin configure -ul --solana-validator-debt-write-off-feature-activation-epoch $SOLANA_VALIDATOR_DEBT_WRITE_OFF_ACTIVATION_EPOCH"
$ADMIN_CLI_BIN configure -ul --solana-validator-debt-write-off-feature-activation-epoch $SOLANA_VALIDATOR_DEBT_WRITE_OFF_ACTIVATION_EPOCH

### Begin tests.

$CLI_BIN -h
echo

### Initialize.

echo "doublezero-solana-validator-debt initialize-distribution -h"
$CLI_BIN initialize-distribution -h
echo

echo "doublezero-solana-validator-debt initialize-distribution -v -ul --dz-env mainnet-beta --bypass-dz-epoch-check --record-debt-accountant ${MAINNET_BETA_DEBT_ACCOUNTANT_KEY} --with-compute-unit-price 1000"
$CLI_BIN initialize-distribution \
    -v \
    -ul \
    --dz-env mainnet-beta \
    --bypass-dz-epoch-check \
    --record-debt-accountant $MAINNET_BETA_DEBT_ACCOUNTANT_KEY \
    --with-compute-unit-price 1000
echo

### In --god-mode, the time to wait for a new initialized distribution is one
### minute.
echo "sleep 60"
sleep 60

echo "doublezero-solana-validator-debt initialize-distribution -v -ul --dz-env mainnet-beta --bypass-dz-epoch-check --record-debt-accountant ${MAINNET_BETA_DEBT_ACCOUNTANT_KEY} --with-compute-unit-price 1000"
$CLI_BIN initialize-distribution \
    -v \
    -ul \
    --dz-env mainnet-beta \
    --bypass-dz-epoch-check \
    --record-debt-accountant $MAINNET_BETA_DEBT_ACCOUNTANT_KEY \
    --with-compute-unit-price 1000
echo
