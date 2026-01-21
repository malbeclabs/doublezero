# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

- remove dz_ledger as argument ([#255](https://github.com/doublezerofoundation/doublezero-offchain/pull/255))
- add check and filter for 0 total debt messages posted to slack ([#247](https://github.com/doublezerofoundation/doublezero-offchain/pull/247))

## [v0.1.6] - 2026-01-12

## [v0.1.5] - 2026-01-08

- shutdown `:normal` after successful distribution initialized ([#245](https://github.com/doublezerofoundation/doublezero-offchain/pull/245))
- add compute unit price handling for wallet ([#243](https://github.com/doublezerofoundation/doublezero-offchain/pull/243))
- update return value from pay_debt command, add pay_debt_for_all_epochs, use :normal exit for GenServer ([#228](https://github.com/doublezerofoundation/doublezero-offchain/pull/228))
- remove unnecessary private functions in lib.rs ([#238](https://github.com/doublezerofoundation/doublezero-offchain/pull/238))
- update `initialize_distribution` call ([#237](https://github.com/doublezerofoundation/doublezero-offchain/pull/237))

## [v0.1.4](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/offchain-scheduler/v0.1.4)

- inline initialize distribution call and remove ledger RPC argument ([#225](https://github.com/doublezerofoundation/doublezero-offchain/pull/225))
- add prom metrics collector and instrument a few critical functions as well as add a `health_check` endpoint ([#207](https://github.com/doublezerofoundation/doublezero-offchain/pull/207))
- summarize debt for each epoch and then for all epochs ([#218](https://github.com/doublezerofoundation/doublezero-offchain/pull/218))

## [v0.1.3](https://github.com/doublezerofoundation/doublezero-offchain/releases/tag/offchain-scheduler/v0.1.3)

- add deploy steps through actions and goreleaser ([#205](https://github.com/doublezerofoundation/doublezero-offchain/pull/205))
- update calculate distribution GenServer to finalize distribution through a Rust NIF ([#200](https://github.com/doublezerofoundation/doublezero-offchain/pull/200))
- add GenServer and Rust NIF to automatically calculate a distribution on a configurable interval ([#199](https://github.com/doublezerofoundation/doublezero-offchain/pull/199))
- add GenServer and Rust NIF to automatically initialize a distribution on a configurable interval ([#197](https://github.com/doublezerofoundation/doublezero-offchain/pull/197))
- add GenServer and Rust NIF to automatically collect debt on a configurable interval ([#183](https://github.com/doublezerofoundation/doublezero-offchain/pull/183))
- add Elixir app that manages scheduling and executing Rust processes for debt collection and payment ([#183](https://github.com/doublezerofoundation/doublezero-offchain/pull/183))
