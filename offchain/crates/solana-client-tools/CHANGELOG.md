# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
