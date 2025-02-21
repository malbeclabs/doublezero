#!/bin/bash

cargo build-bpf

solana-test-validator --reset --bpf-program ./target/deploy/double_zero_sla_program-keypair.json ./target/deploy/double_zero_sla_program.so


