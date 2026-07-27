# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

- add `parse_metro_history_price_at_epoch` and `parse_device_history_premium_at_epoch` for reading a `MetroHistory`/`DeviceHistory` ring entry at a specific epoch, mirroring the onchain `RingBuffer::find` (backwards walk from `current_index` bounded by `total_count`, so `epoch == 0` cannot match a zero-initialized slot), plus `seat_usdc_price_dollars` mirroring `DeviceSubscription::usdc_price_dollars`. `parse_metro_history` / `parse_device_history` keep returning the newest entry (#405)
- migrate to Solana 3.0: workspace `solana-*` crates and `solana-sdk` move to the 3.0 line, `solana-program-test` to 3.0.12, and the doublezero SDK git-deps repin from `client/v0.27.1` to the malbeclabs/doublezero#3830 merge revision (malbeclabs/infra#1853)
- remove `build_memo_instruction` (moved to `Wallet::build_memo_instruction` in `solana-client-tools`, alongside the memo compute-unit helpers)
- add `find_claim_holding_address` PDA helper and `CLAIM_HOLDING_SEED_PREFIX` constant for `ValidatorClientRewards` claim holding accounts
- add `ValidatorClientRewards` discriminator + offset constants and `parse_validator_client_rewards` parser
- add `parse_program_config_shred_oracle_key` helper for reading `ProgramConfig.shred_oracle_key`
- add `InitializeClaimHolding` and `ClaimValidatorClientRewards` instruction variants and the `ClaimHoldingId` Borsh struct (rename mirrors on-chain `shred-subscription/v0.6.6`: discriminator string `dz::ix::initialize_claim_holding_account` → `dz::ix::initialize_claim_holding`)
- add `InitializeClaimHoldingAccounts` builder for the `InitializeClaimHolding` instruction
- add `ClaimValidatorClientRewardsAccounts` builder for the `ClaimValidatorClientRewards` instruction (6 fixed + N holding accounts)
- add shred-subscription publisher-rewards SDK surface for offchain consumers:
  - `ValidatorPublisherRewards` and `ShredRewardToken` `Pod` types with `PrecomputedDiscriminator` impls, plus `state::find_validator_publisher_rewards_address` and `state::find_shred_reward_token_address` PDA helpers.
  - `instruction::ShredSubscriptionInstructionData::{InitializeValidatorPublisherRewards, ConfigureValidatorPublisherRewards}` variants with discriminators and Borsh round-trip.
  - `instruction::account::{InitializeValidatorPublisherRewardsAccounts, ConfigureValidatorPublisherRewardsAccounts}` account-list builders.
  - `instruction::ValidatorOffchainAuthorization` envelope carrying an ed25519 signature + deadline-slot for the offchain auth path.
  - new `types::ConfigureValidatorPublisherRewardsAuthMessage` mirroring the on-chain canonical bytes (sha256 over `DOMAIN_TAG || bytemuck::bytes_of(self)`, hex-encoded for `solana sign-offchain-message`).
- add `bytemuck` and `hex` dependencies (and `solana-offchain-message` dev-dependency) for shred-subscription publisher-rewards sign/verify round-trip tests
- add `RequestProratedInstantSeatWithdrawal` instruction variant and accounts builder
- add `find_shred_distribution_address` PDA helper and `parse_client_seat_last_usdc_price_dollars` parser for prorated withdrawal integration
- add `is_prorated_service_enabled` helper and `ProgramConfig` flag/offset constants for raw-byte parsing
- add `RequestInstantSeatWithdrawal` instruction builder and `withdraw_seat_request` PDA helper
- add reservation module: PDA helpers, instruction builders, and account parsers for the seat reservation program
- add more revenue-distribution fetch methods ([#243](https://github.com/doublezerofoundation/doublezero-offchain/pull/243))
- add `build_memo_instruction` ([#232](https://github.com/doublezerofoundation/doublezero-offchain/pull/232))
- add fetch submodule ([#231](https://github.com/doublezerofoundation/doublezero-offchain/pull/231))
- re-export Passport and Revenue Distribution program interfaces ([#225](https://github.com/doublezerofoundation/doublezero-offchain/pull/225))