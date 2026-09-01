# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

- test(contributor-rewards): fix the Shapley golden tests. `assert_close` now treats both values as equal once each is below `1e-6`, so the seven per-city entries that are cancellation noise (values around `1e-12`) no longer sit below their own comparison gate. `UPDATE_GOLDEN` now regenerates only when set to `1`, not on any set value, so a stale exported `UPDATE_GOLDEN=0` can no longer make both tests pass while silently overwriting the goldens. The fixture and goldens rename from `mn-beta` to `mainnet-beta`, matching the no-abbreviations rule
- test(contributor-rewards): extend the Shapley golden to per-city outputs. The aggregate can hide drift when two cities move in opposite directions, so the per-city values are pinned as well
- test(contributor-rewards): pin the aggregated Shapley output for the committed mainnet-beta fixture with a golden file, the crate's first test covering reward values. Drives `PreparedData::from_snapshot`, the same path the scheduler uses for snapshots. Structure (operator set, ordering, counts) is asserted exactly. Values use a 1e-12 relative tolerance, because bit-identical floating point is not guaranteed across architectures and an exact gate would go permanently red on a CI architecture change. Regenerate deliberately with `UPDATE_GOLDEN=1 cargo test -p doublezero-contributor-rewards`
- test(contributor-rewards): add a committed mainnet-beta snapshot fixture (`tests/goldens/mainnet-beta-epoch-129-trimmed.json`) and the script that produces it (`tests/goldens/make-fixture.py`), for a future Shapley golden test. The only previously committed snapshot (testnet) produces zero reward for every operator, so no golden test can assert a real value against it. Exact Shapley computation is O(2^n) in the operator count, so the fixture keeps only the 4 contributors with the most devices and the 6 cities with the most surviving devices among them, which keeps the network connected and the computation fast. It is not a byte-size trim of the full topology (malbeclabs/infra#2392)
- fix(contributor-rewards): migrate the access pass status value `Expired` to `ExpiredDeprecated` when loading a snapshot captured before doublezero-serviceability PR #3831 renamed that enum variant. Without the migration, such a snapshot fails to deserialize (malbeclabs/infra#2392)
- fix(contributor-rewards): the scheduler no longer writes a snapshot it cannot use. A failed leader-schedule fetch was warned and discarded, so an unusable snapshot overwrote the epoch's canonical S3 key and the tick then failed reading it back with "Missing leader schedule". Both producers now propagate the fetch error, and the scheduler validates before saving, which also covers `--dry-run`, where nothing validated at all. Scheduler failures log the full cause chain, and every `EpochFinder` RPC error is stripped of its request URL, in the retry logs and in the error it propagates, since that URL carries the mainnet-beta read endpoint's API key into journald and Loki (malbeclabs/infra#2372)
- fix(contributor-rewards): resolve the Solana epoch for a timestamp from real block times instead of dividing wall clock by a hardcoded 400ms slot duration. The old estimate drifted about 30k slots per day of lookback and picked the wrong epoch near a boundary, and no fixed constant survives the SIMD-0525 rollout. That epoch selects the leader schedule rewards are computed against, so the search now errors rather than returning a wrong answer: a backfill older than the endpoint's ledger retention fails on the `ingestor::demand` path instead of silently mis-estimating (malbeclabs/infra#2317)
- fix(contributor-rewards): `snapshot` validates before writing. It warns and continues when the leader schedule cannot be fetched, but every consumer rejects a snapshot without one, so the command exited 0 having written an unusable file under the canonical name and a `snapshot` then `export-shapley` chain failed a step late. Pre-existing, but reachable now that resolving the Solana epoch depends on block-time reads (malbeclabs/infra#2317)
- migrate to Solana 3.0: workspace `solana-*` crates and `solana-sdk` move to the 3.0 line, `solana-program-test` to 3.0.12, and the doublezero SDK git-deps repin from `client/v0.27.1` to the malbeclabs/doublezero#3830 merge revision (malbeclabs/infra#1853)
- release artifact now builds as a static `x86_64-unknown-linux-musl` binary (malbeclabs/infra#1853)
- TLS for HTTP clients moves from openssl to rustls; trust roots are the bundled webpki Mozilla set plus the host OS certificate store, so OS-installed private CAs remain trusted (malbeclabs/infra#1853)
- refactor(contributor-rewards): use the shared `Wallet` memo helpers from `solana-client-tools` in place of the local `RELAY_MEMO_CU` constant
- fix(contributor-rewards): backfill `User.feed_pk` (zero pubkey) in the snapshot compat migration so snapshots captured before doublezero#4030 still deserialize (malbeclabs/infra#1853)
- fix(contributor-rewards): handle a serviceability account that surfaces as a decode `Err` per-type instead of aborting the whole fetch mid-loop. AccessPass (reward-neutral) decode failures are warned and skipped; every other, reward-bearing type fails the epoch loudly after warning each bad account, since a partial snapshot would feed Shapley a shrunk graph and freeze a skewed merkle. Decode failures increment a `serviceability_decode_errors` metric (labeled by account type) and are surfaced as `DecodeErrors=` in the completion log ([#403](https://github.com/doublezerofoundation/doublezero-offchain/issues/403))

## [0.6.1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/contributor-rewards%2Fv0.6.1) - 2026-06-13

- chore(contributor-rewards): bump doublezero client to `v0.27.1` ([#388](https://github.com/doublezerofoundation/doublezero-offchain/pull/388))
- chore(contributor-rewards): converge the doublezero SDK family on `client/v0.25.1`, dropping the duplicate v0.20 lockfile entries; adapt to the v0.25 serviceability layout (flat `Device.interfaces`, renamed `UserStatus::*Deprecated` variants) and extend the snapshot compat migrations to backfill `Device.deprecated_interfaces`, the flat `Interface` projection, and `User.bgp_rtt_ns` ([#379](https://github.com/doublezerofoundation/doublezero-offchain/pull/379))
- refactor(contributor-rewards): use the shared create-ATA compute-unit helper from `solana-client-tools` ([#386](https://github.com/doublezerofoundation/doublezero-offchain/pull/386))

## [0.6.0](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/contributor-rewards%2Fv0.6.0) - 2026-05-31

- fix(contributor-rewards): bump network shapley ([#377](https://github.com/doublezerofoundation/doublezero-offchain/pull/377))

## [0.5.5](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/contributor-rewards%2Fv0.5.5) - 2026-05-20

- release(contributor-rewards): prep v0.5.5 ([#371](https://github.com/doublezerofoundation/doublezero-offchain/pull/371))
- docs(contributor-rewards): document reward calculation methodology ([#370](https://github.com/doublezerofoundation/doublezero-offchain/pull/370))
- feat(contributor-rewards): add configurable public latency multiplier ([#369](https://github.com/doublezerofoundation/doublezero-offchain/pull/369))

## [0.5.4](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/contributor-rewards%2Fv0.5.4) - 2026-05-18

- fix(contributor-rewards) distribution summary reporting ([#366](https://github.com/doublezerofoundation/doublezero-offchain/pull/366))

## [0.5.3](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/contributor-rewards%2Fv0.5.3) - 2026-05-07

- fix(contributor-rewards): update shapley input defaults ([#359](https://github.com/doublezerofoundation/doublezero-offchain/pull/359))

## [0.5.2](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/contributor-rewards%2Fv0.5.2) - 2026-05-05

- feat(contributor-rewards): make demand parameters configurable ([#358](https://github.com/doublezerofoundation/doublezero-offchain/pull/358))

## [0.5.1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/contributor-rewards%2Fv0.5.1) - 2026-05-04

- chore(contributor-rewards): bump to v0.5.1 ([#356](https://github.com/doublezerofoundation/doublezero-offchain/pull/356))
- fix(contributor-rewards): subscriber decoding ([#353](https://github.com/doublezerofoundation/doublezero-offchain/pull/353))

## [0.5.0](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/contributor-rewards%2Fv0.5.0) - 2026-04-24

- feat(contributor-rewards): add shred subscription metro price fetching for demand inputs
- feat(contributor-rewards): add support for distribution slack notifications and other minor cleanups ([#285](https://github.com/doublezerofoundation/doublezero-offchain/pull/285))

## [0.4.3](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/contributor-rewards%2Fv0.4.3) - 2026-03-04

- fix(contributor-rewards): stop infinite retry when recipient accounts are missing ([#280](https://github.com/doublezerofoundation/doublezero-offchain/pull/280))

## [0.4.2](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/contributor-rewards%2Fv0.4.2) - 2026-03-04

- feat(contributor-rewards): add on-chain reward distribution ([#269](https://github.com/doublezerofoundation/doublezero-offchain/pull/269))

## [0.4.1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/contributor-rewards%2Fv0.4.1) - 2026-03-03

- feat(contributor-rewards): bump network-shapley to v0.4.0 ([#278](https://github.com/doublezerofoundation/doublezero-offchain/pull/278))

## [0.4.0](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/contributor-rewards%2Fv0.4.0) - 2026-03-03

- feat: billing sentinel for tenant payment status monitoring ([#265](https://github.com/doublezerofoundation/doublezero-offchain/pull/265))
- feat(contributor-rewards): add export shapley command ([#234](https://github.com/doublezerofoundation/doublezero-offchain/pull/234))
- feat(contributor-rewards): add read-rewards command ([#212](https://github.com/doublezerofoundation/doublezero-offchain/pull/212))

## [0.3.5](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/contributor-rewards%2Fv0.3.5) - 2025-11-24

- feat(contributor-rewards): add snapshot flag to inspect shapley cmd ([#209](https://github.com/doublezerofoundation/doublezero-offchain/pull/209))
- fix(contributor-rewards): track shapley output record address for slack notifications ([#208](https://github.com/doublezerofoundation/doublezero-offchain/pull/208))

## [0.3.4](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/contributor-rewards%2Fv0.3.4) - 2025-11-21

- feat(contributor-rewards): add support to send slack notifications ([#206](https://github.com/doublezerofoundation/doublezero-offchain/pull/206))

## [0.3.3](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/contributor-rewards%2Fv0.3.3) - 2025-11-20

- feat(contributor-rewards): add granular support to skip writes ([#203](https://github.com/doublezerofoundation/doublezero-offchain/pull/203)
- fix(contributor-rewards): add Distribution merkle root check to idempotency ([#202](https://github.com/doublezerofoundation/doublezero-offchain/pull/202))

## [0.3.2](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/contributor-rewards%2Fv0.3.2) - 2025-11-17

- fix(contributor-rewards): make scheduler retry infinitely ([#198](https://github.com/doublezerofoundation/doublezero-offchain/pull/198))

## [0.3.1-rc1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/contributor-rewards%2Fv0.3.1-rc1) - 2025-11-11

- feat(solana-cli): add `revenue-distribution fetch distribution --view` argument ([#182](https://github.com/doublezerofoundation/doublezero-offchain/pull/182))
- move binary from /usr/local/bin/ to /usr/bin to comply with package management standards ([#187](https://github.com/doublezerofoundation/doublezero-offchain/pull/187))
- fix(contributor-rewards): handle grace period for scheduling rewards ([#186](https://github.com/doublezerofoundation/doublezero-offchain/pull/186))

## [0.3.0-rc1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/contributor-rewards%2Fv0.3.0-rc1) - 2025-11-04

- fix(contributor-rewards): ci fix to derive default ([#176](https://github.com/doublezerofoundation/doublezero-offchain/pull/176))
- feat(contributor-rewards): Add S3 storage for snapshots ([#174](https://github.com/doublezerofoundation/doublezero-offchain/pull/174))

## [0.2.1-rc1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/contributor-rewards%2Fv0.2.1-rc1) - 2025-10-21

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
