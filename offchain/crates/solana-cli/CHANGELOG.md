# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

- `revenue-distribution fetch validator-debts`: remove the written-off debt sanity check that aborted the command. It compared a windowed sum (the last 100 epochs of debt records) against the deposit's lifetime cumulative `written_off_sol_debt`, so it fired whenever a validator had a write-off older than the window (#380)
- `passport`: route verbs through a typed `PassportCliError` (no more `"{e:#}"` cause-chain flattening), remove the remaining `.expect()`/`.unwrap()` panics, add golden-output tests for `fetch --config` and the find-validator gossip warnings, and emit a single combined JSON object when `fetch` is given both `--config` and `--access-request` (#378)

## [0.5.6](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.5.6)

- uptick crate to v0.5.6 (#376)
- `shreds publisher-rewards configure`: after the configure tx lands, scan the last 100 subscription epochs and submit `DistributeValidatorRewards` for any unsettled leaf so the validator's pending rewards land in the same operator session. Skipped under dry-run; per-epoch soft-fail so one bad epoch doesn't tank the rest (#375)
- `solana-client-tools`: `try_fetch_multiple_zero_copy_data` now returns `Vec<Option<T>>`; a single missing or layout-invalid account surfaces as `None` in its slot instead of failing the whole batch (#374)
- `shreds publisher-rewards`: ergonomics pass — `configure` / `prepare-offchain-message` direct auth now uses the global `-k` signer (its pubkey must equal `--node-id`) instead of a separate `--validator-identity-keypair`, plus sensible default refinements across the subcommand group (#373)

## [0.5.5](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.5.5)

- uptick crate to v0.5.5 (#372)
- relax ownership constraint so that a user can manage their connection but oracle is ultimately the authority (#367)
- align `shreds validator-client-rewards init-holding` with on-chain `shred-subscription/v0.6.6` rename: `InitializeClaimHoldingAccount` → `InitializeClaimHolding` (discriminator string `dz::ix::initialize_claim_holding_account` → `dz::ix::initialize_claim_holding`). Required to unbreak `init-holding` after the on-chain redeploy on 2026-05-15 (#365)
- `shreds validator-client-rewards`: relocate existing `set-proportion` behavior into a subcommand group (hidden); preparation for `claim`, `init-holding`, `show` (#365)
- `shreds validator-client-rewards init-holding`: new permissionless command to initialize claim holding accounts for one or more `(subscription_epoch, mint)` pairs under a `ValidatorClientRewards` PDA (#365)
- `shreds validator-client-rewards claim`: new manager-signed command to drain claim holdings into a destination token account. Defaults destination to ATA(manager, mint); override with `--destination-token-account`. Reads `program_config.shred_oracle_key` to set the on-chain rent beneficiary (#365)
- `shreds validator-client-rewards show`: new read-only command. With just `--client-id`, prints the VCR PDA, manager, description, and claim holding count. With `--rewards-token-mint --subscription-epoch <e>...`, also lists per-epoch holding balances (or `(not initialized)`) (#365)
- Add `shreds publisher-rewards` subcommands for validators to configure their on-chain `ValidatorPublisherRewards` (rewards token mint + destination owner) (#360):
  - `init` — permissionless creation of the VPR PDA seeded by validator node identity.
  - `prepare-offchain-message` — print the hex blob to be signed via `solana sign-offchain-message` (with `--json` for scripting and `--valid-for <DURATION>` / `--deadline-slot <ABS>` for the expiry).
  - `configure` — submit the on-chain configure transaction. Two auth paths: direct (the global `-k` signer keypair signs the tx and its pubkey must equal `--node-id`) or offchain (`--signature <BASE58> --deadline-slot <ABS>` carries an ed25519 sig). Auto-inits the VPR PDA if missing. Pre-flights that the rewards token mint is a registered, enabled `ShredRewardToken`. Idempotently creates the rewards ATA (`--rewards-token-owner` over `--rewards-token-mint`) in the same transaction so payouts are immediately deliverable.
  - `show` — print the current VPR fields and the resolved ATA (`get_associated_token_address(owner, mint)`); reports ATA existence as a status line rather than erroring.
- `shreds payments`: extend instruction-data match to cover the new `InitializeValidatorPublisherRewards` and `ConfigureValidatorPublisherRewards` SDK variants (no-op for escrow event accounting) (#360)
- `shreds pay`: integrate prorated instant seat allocation — when the onchain `is_prorated_service_enabled` flag is set, suppress the late-epoch warning; legacy behavior preserved when the flag is unset (#350)
- `shreds pay`: run the client-side min-amount preflight uniformly (previously bypassed in prorated mode); matches the onchain `FundPaymentEscrowUsdc` minimum which is enforced regardless of proration (#368)
- `shreds withdraw`: use `RequestProratedInstantSeatWithdrawal` to receive a prorated USDC refund when the onchain flag is set and the seat has a recorded `last_usdc_price_dollars`; falls back to the legacy instruction when the flag is unset or the seat pre-dates the prorated rollout (#351)
- `shreds withdraw`: bail with a clear error when an instant seat allocation request is in flight for the seat, rather than submitting a transaction that would be rejected onchain (#357)
- `shreds validator-client-rewards show`: when `--rewards-token-mint` is supplied without `--subscription-epoch`, print the manager's ATA address and balance (previously silently no-op) (#365)
- `shreds validator-client-rewards claim`: print per-holding drained breakdown and re-fetch the VCR to report the remaining `claim_holding_count` after the claim transaction lands (#365)
- `shreds validator-client-rewards claim`: split "wrong owner" and "wrong mint" pre-flight checks into distinct error messages so a non-SPL holding is no longer mislabeled as a wrong-mint holding (#365)

## [0.5.3](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.5.3)

- uptick crate to v0.5.3 (#348)
- `shreds withdraw`: allow withdrawing funds from stale seats and add `--funds-only` flag (#347)
- `shreds list`: fall back to showing all seats when no default keypair is found (#346)

## [0.5.2](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.5.2)

- uptick crate to v0.5.2 (#345)
- uptick crate to v0.5.1 (#344)
- add `--withdraw-excess-balance` to `revenue-distribution validator-deposit` (#343)
- `shreds`: prepend `CheckCliVersion` instruction to all write transactions (pay, withdraw, validator-client-rewards) for onchain minimum CLI version enforcement (#342)

## [0.5.1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.5.1)

- `shreds list`: filter by funder (withdraw-authority) by default, add `--all` flag (#328)
- `shreds pay`: block duplicate client IP across devices — prevent creating a seat for an IP that already has an active seat on a different device (#340)
- `shreds validator-client-rewards`: add hidden command to set validator client rewards proportion (#339)

## [0.5.0](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.5.0)

- uptick version 0.5.0 (#337)
- `shreds pay`: allow top-up when multicast user is owned by the shred oracle (#336)
- support env var fallback for all CLI args (#334)
- add memos, instruction sizing (#330)
- fix env USDC mint (#333)
- `shreds pay`: fix re-funded seats not getting instant allocation after tenure was cleared (#332)
- fix transaction batch size checks to include compute budget instructions (#331)
- meaningful keypair errors (#329)
- shreds: check active service before shreds withdraw (#326)
- revenue-distribution: fix `fetch validator-debts` record logic (#327)
- shreds: remove experimental feature flag from shreds subcommands (#325)
- shreds: hide devices with no remaining seats in shreds price by default (#324)
- solana-client-tools: match DZ ledger testnet genesis hash (#323)
- replace get_program_accounts scans with PDA lookups in shreds price (#318)
- fix broken pipe panic when piping output to head/grep (#317)
- solana-client-tools: derive network defaults from -u moniker in shreds CLI (#313)
- shreds: block shreds pay when device has no available seats (#316)
- `shreds pay`: use per-seat price override in client-side price floor check, and allow `--amount 0` (#314)
- derive network defaults from `-u` moniker: resolve DZ Ledger URLs, USDC mint, and keypair path automatically (#313)
- shreds: correct est epochs paid calculation in shreds list (#312)
- solana-sdk: update default shred subscription program id (#310)
- shreds: add shreds payments command (#307)
- shreds: skip instant seat allocation when re-funding an already-active seat (#306)
- shreds: fix settled seats and available seats in price command (#305)
- shreds: fix est epochs unit mismatch in shreds list (#304)
- shreds: warn when paying for shreds late in epoch (#302) (#302)
- shreds: add --dz-ledger-url flag to override dz ledger rpc endpoint (#303)
- shreds: make instant seat allocation and withdrawal the default (#301)
- shreds: fix reservation price --json outputting text (#300)
- shreds: rework shreds list command for better trader UX (#299)
- shreds: enrich price command with device status and seat info (#287)
- shreds: add guards for pay and withdraw commands (#298)
- solana-sdk: make client-seat account writable (#297)
- solana-sdk: make execution controller writable for instant withdrawal (#296)
- solana-sdk: align instruction discriminators with onchain program (#295)
- shreds: add --unsafe-now flag to withdraw for instant seat withdrawal (#294)
- shreds: add --now flag for instant seat allocation (#293)
- reservation: rename command to shreds (#291)
- reservation: combine initialize-seat and fund into pay command (#289)
- reservation: fix execution_controller writable flag for InitializeClientSeat (#284)
- reservation: update SDK and CLI for on-chain USDC custody changes (#282)
- reservation: add CLI commands (initialize-seat, withdraw, list, price) (#276)

## [0.4.1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.4.1) - 2026-02-26

- solana-cli: add `revenue-distribution configure-contributor-rewards` command to update ContributorRewards recipients and (optionally) protocol-management block/allow flags (#257)
- ensure debt is finalized before collection (#268)
- add prepaid 2Z row for `revenue-distribution fetch distribution` (#266)

## [0.4.0](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.4.0) - 2026-01-29

- sum delinquent debt for `revenue-distribution fetch validator-debts` (#260)
- change default leader schedule lookahead from 2 epochs to 1 for `prepare-validator-access` and `request-validator-access` commands (#259)

## [0.3.3](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.3.3) - 2026-01-21

- solana-cli: show validator debt write-off activation epoch in `revenue-distribution fetch config` (#258)
- solana-cli: add `revenue-distribution fetch contributor-rewards` (#254)
- move fetch methods to SDK (#243)
- migrate `harvest-2z` Jupiter integration to authenticated `api.jup.ag` with optional `--jupiter-api-key` (falls back to `lite-api.jup.ag` without a key) (#242)
- update return value from pay_debt command (#228)

## [0.3.2](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.3.2) - 2025-12-29

- uptick version to 0.3.2 (#241)
- handle missing fee fields for `harvest-2z` Jupiter quotes (#239)

## [0.3.1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.3.1) - 2025-12-18

- uptick version to 0.3.1 (#233)
- add memos to `relay distribute-rewards` and `validator-deposit` commands (#232)
- add `--fund-outstanding-debt` to `revenue-distribution validator-deposit` (#231)
- incorporate debt write-off in views (#225)
- use tracing for `revenue-distribution relay` commands (#226)

## [0.3.0](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.3.0) - 2025-11-24

- uptick to v0.3.0 (#210)
- add `revenue-distribution fetch validator-debts` command (#201)
- add shared validator access validation for `prepare-validator-access` and `request-validator-access` commands (#211)

## [0.2.2](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.2.2) - 2025-11-12

- uptick to v0.2.2 (#191)
- correct default limit price for `convert-2z` and `harvest-2z` (#190)
- add `--specific-dex` option for `harvest-2z` (#189)

## [0.2.1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.2.1) - 2025-11-11

- add `revenue-distribution fetch distribution --view` argument (#182)
- add `revenue-distribution harvest-2z` command (#180)
- add `revenue-distribution relay distribute-rewards` command (#173)
- move binary from /usr/local/bin/ to /usr/bin to comply with package management standards (#187)

## [0.2.0](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.2.0) - 2025-10-22

- fixed identity search in Solana leader schedule (#166)
- testing release-plz integration
- simplify leader schedule check (#157)
- add token balances and more info in stdout (#162)
- integrate slack notifications (#161)
- add SOL conversion commands (#159)
- add sol-conversion-admin-cli (#156)
- import from and export to CSV, add verify command, bug fixes (#147)

## [0.1.1](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/doublezero-solana/v0.1.1) - 2025-10-14

- uptick to v0.1.1 (#152)
- bump doublezero-solana-cli version to 0.1.10 (#151)
- fix backup validator leader schedule check output (#150)
- fix instruction data when requesting access (#149)
- display balance for uninitialized deposit account (#137)
- fix validator deposits not found (#135)
- fetch revenue distribution account for epoch (#128)
- handle multiple requests in a transaction (#127)
- fetch solana validator deposit accounts (#125)
- add find validator command and prepare access functionality (#121)
- lamports -> SOL (#115)
- add Solana validator deposit commands (#111)
- add `find` subcommand to locate nodes by ID or IP address (#108)
- handle requests with backup IDs (#105)
- clean up (#104)
