
#!/bin/bash

MAINNET_BETA_DEBT_ACCOUNTANT_KEY=acLisxTpNkoctPZoqssyo58pcdnHzJyRFhod7Wxkz5a

set -eu

# Wait for the Solana fork the caller started in the background. Shared with
# test_doublezero_solana_fork.sh so the two cannot drift apart.
# shellcheck source=lib/wait_for_fork.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/wait_for_fork.sh"
wait_for_fork

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

$DZ_SOLANA_CLI_BIN -ul revenue-distribution fetch config
echo

echo "Current distribution"
echo "--------------------"
echo

$DZ_SOLANA_CLI_BIN -ul revenue-distribution fetch distribution \
    --debt-accountant $MAINNET_BETA_DEBT_ACCOUNTANT_KEY
echo

### Backwards compatibility: the legacy per-verb (trailing) flag form must keep
### working. Mirrors the fetch above but passes --url/-u and the hidden --dz-env
### AFTER the subcommand; output must match the global-flag form.
echo "[back-compat] doublezero-solana revenue-distribution fetch distribution -ul --dz-env mainnet-beta --debt-accountant ..."
$DZ_SOLANA_CLI_BIN revenue-distribution fetch distribution -ul \
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

$DZ_SOLANA_CLI_BIN -ul revenue-distribution fetch config
echo

echo "Current distribution"
echo "--------------------"
echo

$DZ_SOLANA_CLI_BIN -ul revenue-distribution fetch distribution \
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

$DZ_SOLANA_CLI_BIN -ul revenue-distribution fetch config
echo

echo "Current distribution"
echo "--------------------"
echo

$DZ_SOLANA_CLI_BIN -ul revenue-distribution fetch distribution \
    --debt-accountant $MAINNET_BETA_DEBT_ACCOUNTANT_KEY
echo
