
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

### Fund the test wallet on the fork.
echo "Airdropping SOL to test wallet..."
solana airdrop 100 -ul

### Set up environment.

ADMIN_CLI_BIN=target/debug/doublezero-revenue-distribution-admin
CLI_BIN=target/debug/doublezero-solana-validator-debt
DZ_SOLANA_CLI_BIN=target/debug/doublezero-solana

### Mimic 2Z transfers to the Journal's ATA.
spl-token mint -ul J6pQQ3FAcJQeWPPGppWRb4nM8jU3wLyYbRrLh7feMfvd 69000000 7ZQuXUHeeK4HkQCM49fqgqfwcoBvuAiHGm1eUTiUaRep
spl-token mint -ul J6pQQ3FAcJQeWPPGppWRb4nM8jU3wLyYbRrLh7feMfvd 420000 7ZQuXUHeeK4HkQCM49fqgqfwcoBvuAiHGm1eUTiUaRep

echo "doublezero-revenue-distribution-admin fetch-current-epoch -ul"
CURRENT_EPOCH=$($ADMIN_CLI_BIN fetch-current-epoch -ul)
echo $CURRENT_EPOCH

### Activate Solana validator debt write-off feature after the next epoch.
SOLANA_VALIDATOR_DEBT_WRITE_OFF_ACTIVATION_EPOCH=$((CURRENT_EPOCH + 1))

echo "doublezero-revenue-distribution-admin configure -ul --solana-validator-debt-write-off-feature-activation-epoch $SOLANA_VALIDATOR_DEBT_WRITE_OFF_ACTIVATION_EPOCH"
$ADMIN_CLI_BIN configure \
    -ul \
    --solana-validator-debt-write-off-feature-activation-epoch $SOLANA_VALIDATOR_DEBT_WRITE_OFF_ACTIVATION_EPOCH

### Begin tests.

$CLI_BIN -h
echo

echo "Revenue Distribution Program Config"
echo "-----------------------------------"
echo

$DZ_SOLANA_CLI_BIN revenue-distribution fetch config -ul
echo

echo "Current distribution"
echo "--------------------"
echo

$DZ_SOLANA_CLI_BIN revenue-distribution fetch distribution \
    -ul \
    --dz-env mainnet-beta \
    --debt-accountant $MAINNET_BETA_DEBT_ACCOUNTANT_KEY
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

echo "Revenue Distribution Program Config"
echo "-----------------------------------"
echo

$DZ_SOLANA_CLI_BIN revenue-distribution fetch config -ul
echo

echo "Current distribution"
echo "--------------------"
echo

$DZ_SOLANA_CLI_BIN revenue-distribution fetch distribution \
    -ul \
    --dz-env mainnet-beta \
    --debt-accountant $MAINNET_BETA_DEBT_ACCOUNTANT_KEY
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

echo "Revenue Distribution Program Config"
echo "-----------------------------------"
echo

$DZ_SOLANA_CLI_BIN revenue-distribution fetch config -ul
echo

echo "Current distribution"
echo "--------------------"
echo

$DZ_SOLANA_CLI_BIN revenue-distribution fetch distribution \
    -ul \
    --dz-env mainnet-beta \
    --debt-accountant $MAINNET_BETA_DEBT_ACCOUNTANT_KEY
echo
