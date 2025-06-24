#!/usr/bin/env bash
set -euo pipefail

# Start the solana validator with some noisy output filtered out.
# Configuration and data available at /test-ledger
# Validator logging available at /test-ledger/validator.log
script -q -c "solana-test-validator 2>&1" /dev/null | grep --line-buffered -v "Processed Slot: "
