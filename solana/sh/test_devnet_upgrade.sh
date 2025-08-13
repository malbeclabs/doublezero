#!/bin/bash

set -eu

ROOT_DIR=$(cd "$(dirname "$0")/.."; pwd)

### First build the Revenue Distribution program.
cd $ROOT_DIR/programs/revenue-distribution && \
    cargo build-sbf -- --features development,entrypoint

cd $ROOT_DIR

### Upgrade Revenue Distribution program.
solana program deploy \
    -u l \
    --program-id dzrevZC94tBLwuHw1dyynZxaXTWyp7yocsinyEVPtt4 \
    target/deploy/doublezero_revenue_distribution.so

CLI_BIN=target/release/2z

echo "Waiting 15 seconds for program upgrade to finalize"
sleep 15

### Execute `admin migrate-program-accounts`.
$CLI_BIN admin migrate-program-accounts -u l -v
