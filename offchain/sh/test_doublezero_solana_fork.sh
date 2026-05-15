#!/bin/bash

GENESIS_DZ_EPOCH=31

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

echo "doublezero-solana passport request-validator-access -ul -v --primary-validator-id $NODE_ID --signature $SIGNATURE --doublezero-address $DUMMY_KEY --leader-schedule-epochs 1"
$CLI_BIN passport request-validator-access \
    -ul \
    -v \
    --primary-validator-id $NODE_ID \
    --signature $SIGNATURE \
    --doublezero-address $DUMMY_KEY \
    --leader-schedule-epochs 1
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

echo "doublezero-solana revenue-distribution fetch distribution -ul --dz-env mainnet-beta"
$CLI_BIN revenue-distribution fetch distribution -ul --dz-env mainnet-beta
echo

echo "doublezero-solana revenue-distribution fetch distribution -um --dz-epoch $GENESIS_DZ_EPOCH"
$CLI_BIN revenue-distribution fetch distribution -um --dz-epoch $GENESIS_DZ_EPOCH
echo

echo "doublezero-solana revenue-distribution fetch distribution -um -e $GENESIS_DZ_EPOCH"
$CLI_BIN revenue-distribution fetch distribution -um -e $GENESIS_DZ_EPOCH
echo

echo "doublezero-solana revenue-distribution fetch distribution -um -e $GENESIS_DZ_EPOCH --view summary"
$CLI_BIN revenue-distribution fetch distribution -um -e $GENESIS_DZ_EPOCH --view summary
echo

echo "doublezero-solana revenue-distribution fetch distribution -um -e $GENESIS_DZ_EPOCH --view validator-debt"
$CLI_BIN revenue-distribution fetch distribution -um -e $GENESIS_DZ_EPOCH --view validator-debt
echo

echo "doublezero-solana revenue-distribution fetch distribution -um -e $GENESIS_DZ_EPOCH --view unprocessed-validator-debt"
$CLI_BIN revenue-distribution fetch distribution -um -e $GENESIS_DZ_EPOCH --view unprocessed-validator-debt
echo

echo "doublezero-solana revenue-distribution fetch distribution -um -e $GENESIS_DZ_EPOCH --view written-off-validator-debt"
$CLI_BIN revenue-distribution fetch distribution -um -e $GENESIS_DZ_EPOCH --view written-off-validator-debt
echo

echo "doublezero-solana revenue-distribution fetch distribution -um -e $GENESIS_DZ_EPOCH --view rewards"
$CLI_BIN revenue-distribution fetch distribution -um -e $GENESIS_DZ_EPOCH --view rewards
echo

### Pay outstanding debt for a random validator.
NODE_ID=12i8gndWWWMTRzJBFhnYkobNgZB3XMUUJq75HeUrshrk

echo "doublezero-solana revenue-distribution fetch validator-deposits -ul --node-id $NODE_ID"
$CLI_BIN revenue-distribution fetch validator-deposits -ul --node-id $NODE_ID
echo

echo "doublezero-solana revenue-distribution fetch validator-debts -ul --node-id $NODE_ID --dz-env mainnet-beta"
$CLI_BIN revenue-distribution fetch validator-debts -ul --node-id $NODE_ID --dz-env mainnet-beta
echo

echo "doublezero-solana revenue-distribution validator-deposit --node-id $NODE_ID -ul --fund-outstanding-debt --dz-env mainnet-beta"
$CLI_BIN revenue-distribution validator-deposit \
    --node-id $NODE_ID \
    -ul \
    --fund-outstanding-debt \
    --dz-env mainnet-beta
echo

echo "doublezero-solana revenue-distribution fetch validator-deposits -ul --node-id $NODE_ID"
$CLI_BIN revenue-distribution fetch validator-deposits -ul --node-id $NODE_ID
echo

echo "doublezero-solana revenue-distribution fetch validator-debts -ul --node-id $NODE_ID --dz-env mainnet-beta"
$CLI_BIN revenue-distribution fetch validator-debts -ul --node-id $NODE_ID --dz-env mainnet-beta
echo

echo "doublezero-solana revenue-distribution validator-deposit --withdraw-excess-balance -ul -v --node-id $DUMMY_KEY"
$CLI_BIN revenue-distribution validator-deposit \
    --withdraw-excess-balance \
    -ul \
    -v \
    --node-id $DUMMY_KEY
echo

### Validator-client claim commands.
# Skipped when manager_keypair.json is not present. To exercise this block,
# generate the keypair BEFORE starting the fork loader and pass its pubkey:
#
#   solana-keygen new --silent --no-bip39-passphrase -o manager_keypair.json
#   cargo run --bin doublezero-solana-fork -- -um --reset \
#       --synthetic-vcr-manager $(solana address -k manager_keypair.json)
#   bash sh/test_doublezero_solana_fork.sh
#
# The fork loader bakes a synthetic ValidatorClientRewards PDA at
# `client_id=65535` with the keypair's pubkey as the manager_key.
#
# Note: in v1, the on-chain `InitializeClaimHoldingAccount` handler
# constrains the mint to the 2Z mint. We can't `mint-to` 2Z (no
# authority on mainnet), so the holding stays at balance=0; claim still
# exercises the full ix path and closes the holding, recovering rent.

# Mainnet 2Z mint, hard-coded because the fork is `-um` mainnet.
DOUBLEZERO_MINT=J6pQQ3FAcJQeWPPGppWRb4nM8jU3wLyYbRrLh7feMfvd
MANAGER_KEY_PATH=manager_keypair.json
if [ -f "$MANAGER_KEY_PATH" ]; then
    CLIENT_ID=65535
    MANAGER_PUBKEY=$(solana address -k $MANAGER_KEY_PATH)
    TEST_EPOCH=100

    echo "solana airdrop -ul 1 -k $MANAGER_KEY_PATH"
    solana airdrop -ul 1 -k $MANAGER_KEY_PATH
    echo

    echo "solana-keygen new --silent --no-bip39-passphrase -o claim_payer.json"
    solana-keygen new --silent --no-bip39-passphrase -o claim_payer.json
    solana airdrop -ul 10 -k claim_payer.json
    echo

    echo "spl-token create-account -ul $DOUBLEZERO_MINT --owner $MANAGER_PUBKEY --fee-payer claim_payer.json"
    spl-token create-account \
        -ul \
        $DOUBLEZERO_MINT \
        --owner $MANAGER_PUBKEY \
        --fee-payer claim_payer.json
    echo

    echo "doublezero-solana shreds validator-client-rewards show --client-id $CLIENT_ID -ul"
    $CLI_BIN shreds validator-client-rewards show --client-id $CLIENT_ID -ul
    echo

    echo "doublezero-solana shreds validator-client-rewards init-holding -ul --client-id $CLIENT_ID --rewards-token-mint $DOUBLEZERO_MINT --subscription-epoch $TEST_EPOCH -k claim_payer.json"
    $CLI_BIN shreds validator-client-rewards init-holding \
        -ul \
        --client-id $CLIENT_ID \
        --rewards-token-mint $DOUBLEZERO_MINT \
        --subscription-epoch $TEST_EPOCH \
        -k claim_payer.json
    echo

    echo "doublezero-solana shreds validator-client-rewards show -ul --client-id $CLIENT_ID --rewards-token-mint $DOUBLEZERO_MINT --subscription-epoch $TEST_EPOCH"
    $CLI_BIN shreds validator-client-rewards show \
        -ul \
        --client-id $CLIENT_ID \
        --rewards-token-mint $DOUBLEZERO_MINT \
        --subscription-epoch $TEST_EPOCH
    echo

    echo "doublezero-solana shreds validator-client-rewards claim -ul --client-id $CLIENT_ID --rewards-token-mint $DOUBLEZERO_MINT --subscription-epoch $TEST_EPOCH -k $MANAGER_KEY_PATH"
    $CLI_BIN shreds validator-client-rewards claim \
        -ul \
        --client-id $CLIENT_ID \
        --rewards-token-mint $DOUBLEZERO_MINT \
        --subscription-epoch $TEST_EPOCH \
        -k $MANAGER_KEY_PATH
    echo

    echo "doublezero-solana shreds validator-client-rewards show -ul --client-id $CLIENT_ID --rewards-token-mint $DOUBLEZERO_MINT --subscription-epoch $TEST_EPOCH"
    $CLI_BIN shreds validator-client-rewards show \
        -ul \
        --client-id $CLIENT_ID \
        --rewards-token-mint $DOUBLEZERO_MINT \
        --subscription-epoch $TEST_EPOCH
    echo
else
    echo "Skipping validator-client claim commands: $MANAGER_KEY_PATH not found."
    echo "To exercise this block, generate the keypair before starting the fork loader:"
    echo "  solana-keygen new --silent --no-bip39-passphrase -o $MANAGER_KEY_PATH"
    echo "  cargo run --bin doublezero-solana-fork -- -um --reset --synthetic-vcr-manager \$(solana address -k $MANAGER_KEY_PATH)"
    echo
fi

### Clean up.

echo "rm dummy.json another_payer.json rewards_manager.json " \
     "service_key_1.json validator_node_id.json claim_payer.json"
rm \
    dummy.json \
    another_payer.json \
    rewards_manager.json \
    service_key_1.json
rm -f claim_payer.json
