# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

- add `--synthetic-validator-client-rewards-manager <PUBKEY>` flag that bakes a `ValidatorClientRewards` PDA for `client_id=65535` with the given manager into the fork at genesis, for exercising `shreds validator-client-rewards claim` in fork tests
- build the synthetic `ValidatorClientRewards` account from the SDK's `Pod` mirror instead of copying bytes to hand-written offsets, so its size and field offsets follow the mirror rather than a separate constant table
- the environment variable clap derives from that flag moved with its rename, from `SYNTHETIC_VCR_MANAGER` to `SYNTHETIC_VALIDATOR_CLIENT_REWARDS_MANAGER`. A value left under the old name is ignored rather than rejected, so the fork boots with no synthetic account and `sh/test_doublezero_solana_fork.sh` fails later at the first `validator-client-rewards show`
- Load shred-subscription program and its accounts into the fork so CLI smoke tests (e.g. `shreds publisher-rewards`) can run end-to-end.
- revert: seed journal and fills registry in localnet fork ([#322](https://github.com/doublezerofoundation/doublezero-offchain/pull/322))
- seed journal and fills registry in localnet fork ([#309](https://github.com/doublezerofoundation/doublezero-offchain/pull/309))

## [0.0.1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana-fork-cli/v0.0.1) - 2025-10-22

- fetch journal ATA ([#266](https://github.com/doublezerofoundation/doublezero-offchain/pull/266))
- add `--next-completed-dz-epoch-override` ([#240](https://github.com/doublezerofoundation/doublezero-offchain/pull/240))
- replace `spl-token` with `spl-token-interface` ([#232](https://github.com/doublezerofoundation/doublezero-offchain/pull/232))
- use `doublezero-solana-sdk` as dependency ([#225](https://github.com/doublezerofoundation/doublezero-offchain/pull/225))
- add god mode ([#146](https://github.com/doublezerofoundation/doublezero-offchain/pull/146))
- add doublezero-solana-fork-cli ([#140](https://github.com/doublezerofoundation/doublezero-offchain/pull/140))
