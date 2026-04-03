# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
