# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]
- remove the testnet shred-subscription DZ Ledger special-case: drop `NetworkEnvironment::shred_subscription_url()` and `SolanaConnectionOptions::into_shred_subscription_connection()`. The testnet shred-subscription program now lives on Solana devnet, so callers build a `SolanaConnection` from `-u`/`--url` via the existing `From<SolanaConnectionOptions>` impl (`Wallet::try_new(opts, None)` for signing paths). The `Option<SolanaConnection>` override on `Wallet::try_new` remains for callers that source the connection elsewhere ([infra #1763](https://github.com/malbeclabs/infra/issues/1763))
- add `Wallet::build_memo_instruction`, `Wallet::build_memo_instruction_with_compute_units`, and `Wallet::memo_compute_units` for building spl-memo instructions and estimating their compute units from the memo byte length, calibrated against the spl-memo v3 program in `solana-program-test` (relocated from `solana-sdk`)
- add `Wallet::create_ata_compute_units` and `Wallet::ata_address_and_create_compute_units` helpers for estimating create-ATA compute units ([#386](https://github.com/doublezerofoundation/doublezero-offchain/pull/386))
- add `Devnet` to `NetworkEnvironment`: `-ud`/`devnet` moniker, Solana devnet RPC URL, and genesis-hash detection ([#384](https://github.com/doublezerofoundation/doublezero-offchain/pull/384))
- tolerate missing and unparseable accounts in try_fetch_multiple_zero_copy_data. Return type is now Result<Vec<Option<_>>> (breaking) ([#374](https://github.com/doublezerofoundation/doublezero-offchain/pull/374))
- update solana-cli to handle defaults and tighten up error messages ([#373](https://github.com/doublezerofoundation/doublezero-offchain/pull/373))
- support env var fallback for all CLI args ([#334](https://github.com/doublezerofoundation/doublezero-offchain/pull/334))
- fix transaction batch size checks to include compute budget instructions ([#331](https://github.com/doublezerofoundation/doublezero-offchain/pull/331))
- add in memos, transaction sizing ([#330](https://github.com/doublezerofoundation/doublezero-offchain/pull/330))
- provide meaningful keypair error if invalid or missing ([#329](https://github.com/doublezerofoundation/doublezero-offchain/pull/329))
- match DZ ledger testnet genesis hash ([#323](https://github.com/doublezerofoundation/doublezero-offchain/pull/323))
- derive `Default` for command structs ([#243](https://github.com/doublezerofoundation/doublezero-offchain/pull/243))
- use `unwrap_or_default` for `try_fetch_multiple_accounts` ([#231](https://github.com/doublezerofoundation/doublezero-offchain/pull/231))
- add instruction batching and better network env handling ([#225](https://github.com/doublezerofoundation/doublezero-offchain/pull/225))
- remove tracing feature and log submodule ([#226](https://github.com/doublezerofoundation/doublezero-offchain/pull/226))
- add stdin support for keypair loading ([#217](https://github.com/doublezerofoundation/doublezero-offchain/pull/217))
- add accounts submodule and refactor RPC methods ([#201](https://github.com/doublezerofoundation/doublezero-offchain/pull/201))
- add Solana RPC helpers ([#182](https://github.com/doublezerofoundation/doublezero-offchain/pull/182))

## [0.0.1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana-client-tools/v0.0.1) - 2025-10-21

- add error contexts ([#159](https://github.com/doublezerofoundation/doublezero-offchain/pull/159))
- add better error handling and fix tracing macros ([#156](https://github.com/doublezerofoundation/doublezero-offchain/pull/156))
- port client-tools and admin CLIs from doublezero-solana ([#154](https://github.com/doublezerofoundation/doublezero-offchain/pull/154))
