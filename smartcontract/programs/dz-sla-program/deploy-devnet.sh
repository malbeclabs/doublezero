#!/bin/bash

solana config set --url testnet

cargo build-sbf
solana program deploy ./target/deploy/double_zero_sla_program.so

