# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

- feat(contributor-rewards): add snapshot flag to inspect shapley cmd ([#209](https://github.com/doublezerofoundation/doublezero-offchain/pull/209)
- fix(contributor-rewards): track shapley output record address for slack notifications ([#208](https://github.com/doublezerofoundation/doublezero-offchain/pull/208)

## [0.3.4](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-contributor-rewards/v0.3.4) - 2025-11-21

- feat(contributor-rewards): add support to send slack notifications ([#206](https://github.com/doublezerofoundation/doublezero-offchain/pull/206)

## [0.3.3](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-contributor-rewards/v0.3.3) - 2025-11-20

- feat(contributor-rewards): add granular support to skip writes ([#203](https://github.com/doublezerofoundation/doublezero-offchain/pull/203)
- fix(contributor-rewards): add Distribution merkle root check to idempotency ([#202](https://github.com/doublezerofoundation/doublezero-offchain/pull/202)

## [0.3.2](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-contributor-rewards/v0.3.2) - 2025-11-17

- fix(contributor-rewards): make scheduler retry infinitely ([#198](https://github.com/doublezerofoundation/doublezero-offchain/pull/198)

## [0.3.1-rc1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-contributor-rewards/v0.3.1-rc1) - 2025-11-11

- feat(solana-cli): add `revenue-distribution fetch distribution --view` argument ([#182](https://github.com/doublezerofoundation/doublezero-offchain/pull/182))
- move binary from /usr/local/bin/ to /usr/bin to comply with package management standards ([#187](https://github.com/doublezerofoundation/doublezero-offchain/pull/187))
- fix(contributor-rewards): handle grace period for scheduling rewards ([#186](https://github.com/doublezerofoundation/doublezero-offchain/pull/186))

## [0.3.0-rc1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-contributor-rewards/v0.3.0-rc1) - 2025-11-04

- fix(contributor-rewards): ci fix to derive default [#176](https://github.com/doublezerofoundation/doublezero-offchain/pull/176))
- feat(contributor-rewards): Add S3 storage for snapshots [#174](https://github.com/doublezerofoundation/doublezero-offchain/pull/174))

## [0.2.1-rc1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-contributor-rewards/v0.2.1-rc1) - 2025-10-21

### Other

- testing release-plz integration
- add allow_multiple_ips to access pass args, bump deps ([#158](https://github.com/doublezerofoundation/doublezero-offchain/pull/158))
- add quadratic penalty for uptime ([#148](https://github.com/doublezerofoundation/doublezero-offchain/pull/148))
- Fix deps, fix clippy warning ([#145](https://github.com/doublezerofoundation/doublezero-offchain/pull/145))
- fix reward proportion discrepancies ([#143](https://github.com/doublezerofoundation/doublezero-offchain/pull/143))
- enhance metrics for shapley computations ([#100](https://github.com/doublezerofoundation/doublezero-offchain/pull/100))
- Bump stable rust and fixup clippy warnings ([#109](https://github.com/doublezerofoundation/doublezero-offchain/pull/109))
- handle requests with backup IDs ([#105](https://github.com/doublezerofoundation/doublezero-offchain/pull/105))
- add support to handle AccessPass ([#92](https://github.com/doublezerofoundation/doublezero-offchain/pull/92))
- Fix scheduler for dry-run mode ([#97](https://github.com/doublezerofoundation/doublezero-offchain/pull/97))
- add observability via metrics ([#90](https://github.com/doublezerofoundation/doublezero-offchain/pull/90))
- add scheduler support ([#86](https://github.com/doublezerofoundation/doublezero-offchain/pull/86))
- add telemetry rent cmd ([#81](https://github.com/doublezerofoundation/doublezero-offchain/pull/81))
- add pay debt commands ([#80](https://github.com/doublezerofoundation/doublezero-offchain/pull/80))
- Modular CLI ([#70](https://github.com/doublezerofoundation/doublezero-offchain/pull/70))
- Update revenue_distribution payments to debt ([#75](https://github.com/doublezerofoundation/doublezero-offchain/pull/75))
- rm shapley_input req for writing telem aggs ([#71](https://github.com/doublezerofoundation/doublezero-offchain/pull/71))
- add release support ([#72](https://github.com/doublezerofoundation/doublezero-offchain/pull/72))
- Fix Exchange Code Mappings for Public Links ([#63](https://github.com/doublezerofoundation/doublezero-offchain/pull/63))
- update dependencies and improve access request handling ([#64](https://github.com/doublezerofoundation/doublezero-offchain/pull/64))
- cleanup settings, add example config, CLI docs ([#60](https://github.com/doublezerofoundation/doublezero-offchain/pull/60))
- Derive rewards accountant key from ProgramConfig ([#59](https://github.com/doublezerofoundation/doublezero-offchain/pull/59))
- First pass at CLI polish ([#57](https://github.com/doublezerofoundation/doublezero-offchain/pull/57))
- Fix internet historical telem data lookup ([#56](https://github.com/doublezerofoundation/doublezero-offchain/pull/56))
- Add support to post contributor-rewards merkle root ([#50](https://github.com/doublezerofoundation/doublezero-offchain/pull/50))
- defaults for shapley calculations ([#52](https://github.com/doublezerofoundation/doublezero-offchain/pull/52))
- Switch all maps to BTreeMap and sets to BTreeSet ([#44](https://github.com/doublezerofoundation/doublezero-offchain/pull/44))
- Add historical epoch lookup for internet telemetry data ([#33](https://github.com/doublezerofoundation/doublezero-offchain/pull/33))
- Enhance aggregated telemetry stats ([#30](https://github.com/doublezerofoundation/doublezero-offchain/pull/30))
- Switch to use indexed merkle leaves ([#31](https://github.com/doublezerofoundation/doublezero-offchain/pull/31))
- Build public links using exchange based inet telem data ([#23](https://github.com/doublezerofoundation/doublezero-offchain/pull/23))
- Address review cmt, rm unnecessary import
- Prepare for off-chain components
