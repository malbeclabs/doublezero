# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.2](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/sentinel/v0.2.2) - 2025-11-11

- fix(sentinel): retry on conn reset, one more time [#184](https://github.com/doublezerofoundation/doublezero-offchain/pull/184)
- move binary from /usr/local/bin/ to /usr/bin to comply with package management standards ([#187](https://github.com/doublezerofoundation/doublezero-offchain/pull/187))

## [0.2.1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/sentinel/v0.2.1) - 2025-11-04

- retry on ECONNRESET [#177](https://github.com/doublezerofoundation/doublezero-offchain/pull/177)

## [0.2.0](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/sentinel/v0.2.0) - 2025-10-21

### Fixed

- fix versioned transaction handling; more logging ([#48](https://github.com/doublezerofoundation/doublezero-offchain/pull/48))

### Other

- testing release-plz integration
- entirely remove websocket support ([#160](https://github.com/doublezerofoundation/doublezero-offchain/pull/160))
- simplify leader schedule check ([#157](https://github.com/doublezerofoundation/doublezero-offchain/pull/157))
- add allow_multiple_ips to access pass args, bump deps ([#158](https://github.com/doublezerofoundation/doublezero-offchain/pull/158))
- version 0.1.9 ([#153](https://github.com/doublezerofoundation/doublezero-offchain/pull/153))
- drain outstanding access requests ([#144](https://github.com/doublezerofoundation/doublezero-offchain/pull/144))
- cli flag to control RPC polling interval for access requests ([#136](https://github.com/doublezerofoundation/doublezero-offchain/pull/136))
- fixup sentinel listener init to also use retries ([#132](https://github.com/doublezerofoundation/doublezero-offchain/pull/132))
- fix websocket reconnection, improve error resilience ([#131](https://github.com/doublezerofoundation/doublezero-offchain/pull/131))
- fetch revenue distribution account for epoch ([#128](https://github.com/doublezerofoundation/doublezero-offchain/pull/128))
- handle multiple requests in a transaction ([#127](https://github.com/doublezerofoundation/doublezero-offchain/pull/127))
- add find validator command and prepare access functionality ([#121](https://github.com/doublezerofoundation/doublezero-offchain/pull/121))
- verify_qualifiers return empty vec on SignatureVerify error ([#122](https://github.com/doublezerofoundation/doublezero-offchain/pull/122))
- read access mode from account data instead of transaction ([#107](https://github.com/doublezerofoundation/doublezero-offchain/pull/107))
- handle requests with backup IDs ([#105](https://github.com/doublezerofoundation/doublezero-offchain/pull/105))
- retry all rpc calls on 503 ([#103](https://github.com/doublezerofoundation/doublezero-offchain/pull/103))
- Add retry logic for access pass operations ([#99](https://github.com/doublezerofoundation/doublezero-offchain/pull/99))
- Disable leader schedule check ([#98](https://github.com/doublezerofoundation/doublezero-offchain/pull/98))
- update dependencies and improve access request handling ([#64](https://github.com/doublezerofoundation/doublezero-offchain/pull/64))
- accept mainnet-beta env moniker ([#62](https://github.com/doublezerofoundation/doublezero-offchain/pull/62))
- wrap dz instruction properly ([#54](https://github.com/doublezerofoundation/doublezero-offchain/pull/54))
- wrap message verification in offchain message ([#43](https://github.com/doublezerofoundation/doublezero-offchain/pull/43))
- remove sentinel setting new validator airdrop ([#40](https://github.com/doublezerofoundation/doublezero-offchain/pull/40))
- Jg/validator sig update ([#39](https://github.com/doublezerofoundation/doublezero-offchain/pull/39))
- handle websocket server disconnects ([#37](https://github.com/doublezerofoundation/doublezero-offchain/pull/37))
- update access pass creation to delegate funding to serviceability ([#32](https://github.com/doublezerofoundation/doublezero-offchain/pull/32))
- validator ip fetching and issuing access pass ([#29](https://github.com/doublezerofoundation/doublezero-offchain/pull/29))
- Build public links using exchange based inet telem data ([#23](https://github.com/doublezerofoundation/doublezero-offchain/pull/23))
- migrate sentinel from solana-programs repo ([#21](https://github.com/doublezerofoundation/doublezero-offchain/pull/21))
