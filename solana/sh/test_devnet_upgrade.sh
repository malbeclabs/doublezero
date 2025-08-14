#!/bin/bash

set -eu

ROOT_DIR=$(cd "$(dirname "$0")/.."; pwd)

### Build programs.

cd $ROOT_DIR/programs/passport && \
    cargo build-sbf -- --features development,entrypoint
cd $ROOT_DIR/programs/revenue-distribution && \
    cargo build-sbf -- --features development,entrypoint

cd $ROOT_DIR

### Deploy upgrades.

solana program deploy \
    -u l \
    --program-id dzpt2dM8g9qsLxpdddnVvKfjkCLVXd82jrrQVJigCPV \
    target/deploy/doublezero_passport.so
solana program deploy \
    -u l \
    --program-id dzrevZC94tBLwuHw1dyynZxaXTWyp7yocsinyEVPtt4 \
    target/deploy/doublezero_revenue_distribution.so

CLI_BIN=target/release/2z

echo "Waiting 15 seconds for program upgrades to finalize"
sleep 15

echo "2z admin revenue-distribution migrate-program-accounts -u l -v"
$CLI_BIN admin revenue-distribution migrate-program-accounts -u l -v
