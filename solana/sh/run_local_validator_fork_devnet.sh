#!/bin/bash

set -eu

ROOT_DIR=$(cd "$(dirname "$0")/.."; pwd)

LOCALNET_DIR=$ROOT_DIR/localnet
LOCALNET_CACHE_DIR=$LOCALNET_DIR/cache

REVENUE_DISTRIBUTION_PROGRAM_ID=dzrevZC94tBLwuHw1dyynZxaXTWyp7yocsinyEVPtt4
PASSPORT_PROGRAM_ID=dzpt2dM8g9qsLxpdddnVvKfjkCLVXd82jrrQVJigCPV

mkdir -p $LOCALNET_CACHE_DIR

### Dump program accounts from Solana devnet into localnet/cache.
solana program dump -u d $REVENUE_DISTRIBUTION_PROGRAM_ID $LOCALNET_CACHE_DIR/$REVENUE_DISTRIBUTION_PROGRAM_ID.so
solana program dump -u d $PASSPORT_PROGRAM_ID $LOCALNET_CACHE_DIR/$PASSPORT_PROGRAM_ID.so

DEFAULT_USER_KEYPAIR=$HOME/.config/solana/id.json

if [ ! -f $DEFAULT_USER_KEYPAIR ]; then
    echo "Generating user keypair"
    solana-keygen new --silent --no-bip39-passphrase
fi

USER_KEY=$(solana address)

PROGRAM_CONFIG_KEY=8hCG3Mc1wmCTJYGn4QzFWEmvevonGunRxBTH9qPg1Um9
RESERVE_2Z_KEY=EveENGjayjLJwfwLKckhSdzhvon92k7xJDxk1MhU32Ws
JOURNAL_KEY=2x9zkLfiLAQLYeiibHp2ccSSsF4d8X5UQ3vtBDwbQhuo
JOURNAL_2Z_TOKEN_PDA_KEY=6r3HXh7YrSsB2vYpjW4Qggsj416YU6x9ND3dQQuzWttt

### Run local validator with dumped program accounts and 2Z mint.
solana-test-validator -u d \
    --reset \
    --upgradeable-program \
    $REVENUE_DISTRIBUTION_PROGRAM_ID \
    $LOCALNET_CACHE_DIR/$REVENUE_DISTRIBUTION_PROGRAM_ID.so \
    $USER_KEY \
    --upgradeable-program \
    $PASSPORT_PROGRAM_ID \
    $LOCALNET_CACHE_DIR/$PASSPORT_PROGRAM_ID.so \
    $USER_KEY \
    --clone \
    $PROGRAM_CONFIG_KEY \
    --clone \
    $RESERVE_2Z_KEY \
    --clone \
    $JOURNAL_KEY \
    --clone \
    $JOURNAL_2Z_TOKEN_PDA_KEY
