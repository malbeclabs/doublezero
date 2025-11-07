# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

- feat(solana-cli): add `revenue-distribution fetch distribution --view` argument ([#182](https://github.com/doublezerofoundation/doublezero-offchain/pull/182))
- parse program logs, attach exported csv to slack msg ([#163](https://github.com/doublezerofoundation/doublezero-offchain/pull/163))

## [0.1.0-rc4](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/solana-validator-debt/v0.1.0-rc4) - 2025-10-21

### Other

- testing release-plz integration
- integrate slack notifications ([#161](https://github.com/doublezerofoundation/doublezero-offchain/pull/161))
- add sol-conversion-admin-cli ([#156](https://github.com/doublezerofoundation/doublezero-offchain/pull/156))
- import from and export to CSV, add verify command, bug fixes ([#147](https://github.com/doublezerofoundation/doublezero-offchain/pull/147))
- display balance for uninitialized deposit account ([#137](https://github.com/doublezerofoundation/doublezero-offchain/pull/137))
- default epoch to latest for calculating debt ([#133](https://github.com/doublezerofoundation/doublezero-offchain/pull/133))
- option to post to DZ ledger only ([#130](https://github.com/doublezerofoundation/doublezero-offchain/pull/130))
- update solana epoch finder ([#129](https://github.com/doublezerofoundation/doublezero-offchain/pull/129))
- fetch revenue distribution account for epoch ([#128](https://github.com/doublezerofoundation/doublezero-offchain/pull/128))
- estimate block time if slot is skipped ([#126](https://github.com/doublezerofoundation/doublezero-offchain/pull/126))
- add find Solana epoch command ([#119](https://github.com/doublezerofoundation/doublezero-offchain/pull/119))
- fix fetched epoch ([#118](https://github.com/doublezerofoundation/doublezero-offchain/pull/118))
- add missing mainnet check ([#117](https://github.com/doublezerofoundation/doublezero-offchain/pull/117))
- schedule initializing distributions ([#106](https://github.com/doublezerofoundation/doublezero-offchain/pull/106))
- handle requests with backup IDs ([#105](https://github.com/doublezerofoundation/doublezero-offchain/pull/105))
- handle overlapping Solana epochs ([#96](https://github.com/doublezerofoundation/doublezero-offchain/pull/96))
- add checks after writing to ledger ([#95](https://github.com/doublezerofoundation/doublezero-offchain/pull/95))
- ensure distribution has passed calculation_allowed_timestamp ([#93](https://github.com/doublezerofoundation/doublezero-offchain/pull/93))
- output result of `write_debts` to tabled format ([#88](https://github.com/doublezerofoundation/doublezero-offchain/pull/88))
- add CLI ([#91](https://github.com/doublezerofoundation/doublezero-offchain/pull/91))
- separate `initialize_distribution` into its own process ([#89](https://github.com/doublezerofoundation/doublezero-offchain/pull/89))
- fetch validator pubkeys from access passes ([#82](https://github.com/doublezerofoundation/doublezero-offchain/pull/82))
- Add retry/backoff to Jito/solana RPC calls ([#87](https://github.com/doublezerofoundation/doublezero-offchain/pull/87))
- add pay debt commands ([#80](https://github.com/doublezerofoundation/doublezero-offchain/pull/80))
- Prepare for off-chain components
- Reorg
- Fix api token security, retries and concurrent requests
- Add docs
- More cleanup and simplification
- configuration and defaults
- Cleanup, add TODOs
- Add merkle_generator
- Update README
- Simplify
- Bump README
- Add README
