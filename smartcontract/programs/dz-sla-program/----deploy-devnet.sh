#!/bin/bash

solana config set --url devnet
cargo build-sbf

#solana-keygen new -o ./target/deploy/double_zero_sla_program-keypair.json --force --no-bip39-passphrase
# solana program deploy ./target/deploy/double_zero_sla_program.so

solana program deploy --program-id ./target/deploy/double_zero_sla_program-keypair.json ./target/deploy/double_zero_sla_program.so

#python3 update.py