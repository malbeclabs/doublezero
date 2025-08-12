#!/bin/bash

set -eu

CLI_BIN=target/release/2z

### Check help menus.
$CLI_BIN -h
$CLI_BIN admin -h
$CLI_BIN ata -h
$CLI_BIN contributor -h
$CLI_BIN prepaid -h
$CLI_BIN validator -h

### Establish another payer.
solana-keygen new --silent --no-bip39-passphrase -o another_payer.json
solana airdrop -u l 69 -k another_payer.json

### Execute `admin initialize`.
$CLI_BIN admin initialize -u l -v

### Execute `admin set-admin` without fee payer.
$CLI_BIN admin set-admin -u l -v devgM7SXHvoHH6jPXRsjn97gygPUo58XEnc9bqY1jpj

### Execute `admin set-admin` with fee payer.
$CLI_BIN admin set-admin -u l -v --fee-payer another_payer.json $(solana address)
