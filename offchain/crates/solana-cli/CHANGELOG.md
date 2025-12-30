# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

- handle missing fee fields for `harvest-2z` Jupiter quotes ([#239](https://github.com/doublezerofoundation/doublezero-offchain/pull/239))

## [0.3.1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.3.1) - 2025-12-18

- uptick version to 0.3.1 ([#233](https://github.com/doublezerofoundation/doublezero-offchain/pull/233))
- add memos to `relay distribute-rewards` and `validator-deposit` commands ([#232](https://github.com/doublezerofoundation/doublezero-offchain/pull/232))
- add `--fund-outstanding-debt` to `revenue-distribution validator-deposit` ([#231](https://github.com/doublezerofoundation/doublezero-offchain/pull/231))
- incorporate debt write-off in views ([#225](https://github.com/doublezerofoundation/doublezero-offchain/pull/225))
- use tracing for `revenue-distribution relay` commands ([#226](https://github.com/doublezerofoundation/doublezero-offchain/pull/226))

## [0.3.0](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.3.0) - 2025-11-24

- uptick to v0.3.0 ([#210](https://github.com/doublezerofoundation/doublezero-offchain/pull/210))
- add `revenue-distribution fetch validator-debts` command ([#201](https://github.com/doublezerofoundation/doublezero-offchain/pull/201))
- add shared validator access validation for `prepare-validator-access` and `request-validator-access` commands ([#211](https://github.com/doublezerofoundation/doublezero-offchain/pull/211))

## [0.2.2](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.2.2) - 2025-11-12

- uptick to v0.2.2 ([#191](https://github.com/doublezerofoundation/doublezero-offchain/pull/191))
- correct default limit price for `convert-2z` and `harvest-2z` ([#190](https://github.com/doublezerofoundation/doublezero-offchain/pull/190))
- add `--specific-dex` option for `harvest-2z` ([#189](https://github.com/doublezerofoundation/doublezero-offchain/pull/189))

## [0.2.1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.2.1) - 2025-11-11

- add `revenue-distribution fetch distribution --view` argument ([#182](https://github.com/doublezerofoundation/doublezero-offchain/pull/182))
- add `revenue-distribution harvest-2z` command ([#180](hhttps://github.com/doublezerofoundation/doublezero-offchain/pull/180))
- add `revenue-distribution relay distribute-rewards` command ([#173](https://github.com/doublezerofoundation/doublezero-offchain/pull/173))
- move binary from /usr/local/bin/ to /usr/bin to comply with package management standards ([#187](https://github.com/doublezerofoundation/doublezero-offchain/pull/187))

## [0.2.0](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.2.0) - 2025-10-22

- fixed identity search in Solana leader schedule ([#166](https://github.com/doublezerofoundation/doublezero-offchain/pull/166))
- testing release-plz integration
- simplify leader schedule check ([#157](https://github.com/doublezerofoundation/doublezero-offchain/pull/157))
- add token balances and more info in stdout ([#162](https://github.com/doublezerofoundation/doublezero-offchain/pull/162))
- integrate slack notifications ([#161](https://github.com/doublezerofoundation/doublezero-offchain/pull/161))
- add SOL conversion commands ([#159](https://github.com/doublezerofoundation/doublezero-offchain/pull/159))
- add sol-conversion-admin-cli ([#156](https://github.com/doublezerofoundation/doublezero-offchain/pull/156))
- import from and export to CSV, add verify command, bug fixes ([#147](https://github.com/doublezerofoundation/doublezero-offchain/pull/147))

## [0.1.1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.1.1) - 2025-10-14

- uptick to v0.1.1 ([#152](https://github.com/doublezerofoundation/doublezero-offchain/pull/152))
- bump doublezero-solana-cli version to 0.1.10 ([#151](https://github.com/doublezerofoundation/doublezero-offchain/pull/151))
- fix backup validator leader schedule check output ([#150](https://github.com/doublezerofoundation/doublezero-offchain/pull/150))
- fix instruction data when requesting access ([#149](https://github.com/doublezerofoundation/doublezero-offchain/pull/149))
- display balance for uninitialized deposit account ([#137](https://github.com/doublezerofoundation/doublezero-offchain/pull/137))
- fix validator deposits not found ([#135](https://github.com/doublezerofoundation/doublezero-offchain/pull/135))
- fetch revenue distribution account for epoch ([#128](https://github.com/doublezerofoundation/doublezero-offchain/pull/128))
- handle multiple requests in a transaction ([#127](https://github.com/doublezerofoundation/doublezero-offchain/pull/127))
- fetch solana validator deposit accounts ([#125](https://github.com/doublezerofoundation/doublezero-offchain/pull/125))
- add find validator command and prepare access functionality ([#121](https://github.com/doublezerofoundation/doublezero-offchain/pull/121))
- lamports -> SOL ([#115](https://github.com/doublezerofoundation/doublezero-offchain/pull/115))
- add Solana validator deposit commands ([#111](https://github.com/doublezerofoundation/doublezero-offchain/pull/111))
- add `find` subcommand to locate nodes by ID or IP address ([#108](https://github.com/doublezerofoundation/doublezero-offchain/pull/108))
- handle requests with backup IDs ([#105](https://github.com/doublezerofoundation/doublezero-offchain/pull/105))
- clean up ([#104](https://github.com/doublezerofoundation/doublezero-offchain/pull/104))