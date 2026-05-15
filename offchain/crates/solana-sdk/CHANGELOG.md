# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

- add `find_claim_holding_address` PDA helper and `CLAIM_HOLDING_SEED_PREFIX` constant for `ValidatorClientRewards` claim holding accounts
- add `ValidatorClientRewards` discriminator + offset constants and `parse_validator_client_rewards` parser
- add `parse_program_config_shred_oracle_key` helper for reading `ProgramConfig.shred_oracle_key`
- add `InitializeClaimHoldingAccount` and `ClaimValidatorClientRewards` instruction variants and the `ClaimHoldingId` Borsh struct
- add `InitializeClaimHoldingAccountAccounts` builder for the `InitializeClaimHoldingAccount` instruction
- add `ClaimValidatorClientRewardsAccounts` builder for the `ClaimValidatorClientRewards` instruction (6 fixed + N holding accounts)
- add `RequestProratedInstantSeatWithdrawal` instruction variant and accounts builder
- add `find_shred_distribution_address` PDA helper and `parse_client_seat_last_usdc_price_dollars` parser for prorated withdrawal integration
- add `is_prorated_service_enabled` helper and `ProgramConfig` flag/offset constants for raw-byte parsing
- add `RequestInstantSeatWithdrawal` instruction builder and `withdraw_seat_request` PDA helper
- add reservation module: PDA helpers, instruction builders, and account parsers for the seat reservation program
- add more revenue-distribution fetch methods ([#243](https://github.com/doublezerofoundation/doublezero-offchain/pull/243))
- add `build_memo_instruction` ([#232](https://github.com/doublezerofoundation/doublezero-offchain/pull/232))
- add fetch submodule ([#231](https://github.com/doublezerofoundation/doublezero-offchain/pull/231))
- re-export Passport and Revenue Distribution program interfaces ([#225](https://github.com/doublezerofoundation/doublezero-offchain/pull/225))