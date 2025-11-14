# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).


## [Unreleased]
- feat(automate_initialize_distribution): Add GenServer and Rust NIF to automatically initialiaze a distribution on a configurable interval ([#197]https://github.com/doublezerofoundation/doublezero-offchain/pull/197)
- feat(automate_debt_payment): Add GenServer and Rust NIF to automatically collect debt on a configurable interval ([#183]https://github.com/doublezerofoundation/doublezero-offchain/pull/183)
- feat(scheduler): add Elixir app that manages scheduling and executing Rust processes for debt collection and payment  ([#183]https://github.com/doublezerofoundation/doublezero-offchain/pull/183)