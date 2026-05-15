# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

- add `--synthetic-vcr-manager <PUBKEY>` flag that bakes a `ValidatorClientRewards` PDA for `client_id=65535` with the given manager into the fork at genesis, for exercising `shreds validator-client-rewards claim` in fork tests
- fix: synthetic VCR account body is 184 bytes — `StorageGap<2>` is 64 bytes (not 48); the prior 176-byte account was rejected at runtime by the on-chain `data_end == 184` const assertion
- revert: seed journal and fills registry in localnet fork ([#322](https://github.com/doublezerofoundation/doublezero-offchain/pull/322))
- seed journal and fills registry in localnet fork ([#309](https://github.com/doublezerofoundation/doublezero-offchain/pull/309))

## [0.0.1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana-fork-cli/v0.0.1) - 2025-10-22

- fetch journal ATA ([#266](https://github.com/doublezerofoundation/doublezero-offchain/pull/266))
- add `--next-completed-dz-epoch-override` ([#240](https://github.com/doublezerofoundation/doublezero-offchain/pull/240))
- replace `spl-token` with `spl-token-interface` ([#232](https://github.com/doublezerofoundation/doublezero-offchain/pull/232))
- use `doublezero-solana-sdk` as dependency ([#225](https://github.com/doublezerofoundation/doublezero-offchain/pull/225))
- add god mode ([#146](https://github.com/doublezerofoundation/doublezero-offchain/pull/146))
- add doublezero-solana-fork-cli ([#140](https://github.com/doublezerofoundation/doublezero-offchain/pull/140))
