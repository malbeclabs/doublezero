#!/bin/sh

# Check for required environment variables.
if [ -z "${DZ_PROGRAM_PATH}" ]; then
  echo "DZ_PROGRAM_PATH is not set"
  exit 1
fi
if [ -z "${DZ_PROGRAM_KEYPAIR_PATH}" ]; then
  echo "DZ_PROGRAM_KEYPAIR_PATH is not set"
  exit 1
fi

# If DEBUG is set, include the --log flag.
if [ -n "${DEBUG}" ]; then
  LOG_FLAG="--log"
fi

solana config set --url http://127.0.0.1:8899 --ws ws://127.0.0.1:8900

# Start the solana validator with the doublezero program.
# NOTE: The output can be noisy so we filter out the "Processed Slot: " messages.
script -q -c "solana-test-validator --reset --bpf-program ${DZ_PROGRAM_KEYPAIR_PATH} ${DZ_PROGRAM_PATH} ${LOG_FLAG} 2>&1" /dev/null | grep --line-buffered -v "Processed Slot: "
