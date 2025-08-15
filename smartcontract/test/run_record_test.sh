#!/bin/bash

export TICKS_PER_SLOT=16

set -eu

if pgrep -f "solana-test-validator" > /dev/null; then
    echo "Error: solana-test-validator is already running"
    pgrep "solana-test-validator"
    exit 1
fi

SMARTCONTRACT_DIR=$(cd "$(dirname "$0")/.."; pwd)

echo "Build DoubleZero Record program"
cd $SMARTCONTRACT_DIR/programs/doublezero-record
cargo build-sbf

ROOT_DIR=$SMARTCONTRACT_DIR/..

echo "Start Solana test validator"
solana-test-validator -r \
    --ticks-per-slot $TICKS_PER_SLOT \
    --bpf-program dzrecxigtaZQ3gPmt2X5mDkYigaruFR1rHCqztFTvx7 $ROOT_DIR/target/deploy/doublezero_record.so \
    > /dev/null 2>&1 &
VALIDATOR_PID=$!

cleanup() {
    if [ ! -z "$VALIDATOR_PID" ] && kill -0 $VALIDATOR_PID 2>/dev/null; then
        kill $VALIDATOR_PID
    fi
}

trap cleanup EXIT INT TERM

cd $SMARTCONTRACT_DIR/sdk/rs
cargo test --test '*' --features local-validator-test