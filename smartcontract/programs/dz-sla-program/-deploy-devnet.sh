#!/bin/bash

solana config set --url devnet
cargo build-sbf

solana program deploy --program-id ./target/deploy/double_zero_sla_program-keypair.json ./target/deploy/double_zero_sla_program.so

#python3 update.py