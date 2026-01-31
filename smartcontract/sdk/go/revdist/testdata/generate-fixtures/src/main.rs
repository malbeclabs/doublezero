//! Generates binary fixture files from the Rust revenue-distribution structs
//! with known field values. The Go SDK compatibility tests deserialize these
//! fixtures and verify that field values match.
//!
//! Run with: cargo run (from this directory)
//! Output: ../fixtures/*.bin and ../fixtures/*.json
//!
//! The key property: bytes are produced by `bytemuck::bytes_of` on real Rust
//! struct instances with fields set via struct access. The byte layout is
//! therefore determined by the Rust compiler's `#[repr(C)]` layout — NOT by
//! hand-coded offsets. This makes the fixtures authoritative for cross-language
//! compatibility testing.

use std::fs;
use std::path::Path;

use bytemuck::bytes_of;
use doublezero_program_tools::PrecomputedDiscriminator;
use doublezero_revenue_distribution::state::{
    ContributorRewards, Distribution, Journal, ProgramConfig, SolanaValidatorDeposit,
};
use doublezero_revenue_distribution::types::{BurnRate, DoubleZeroEpoch, ValidatorFee};
use serde::Serialize;
use ruint::aliases::U64;
use solana_pubkey::Pubkey;

#[derive(Serialize)]
struct FixtureMeta {
    name: String,
    struct_size: usize,
    discriminator_hex: String,
    fields: Vec<FieldValue>,
}

#[derive(Serialize)]
struct FieldValue {
    name: String,
    value: String,
    /// "u8", "u16", "u32", "u64", "pubkey"
    typ: String,
}

fn pubkey_from_byte(b: u8) -> Pubkey {
    let mut bytes = [0u8; 32];
    bytes[0] = b;
    Pubkey::from(bytes)
}

fn write_fixture(
    dir: &Path,
    name: &str,
    discriminator: &[u8],
    struct_bytes: &[u8],
    meta: &FixtureMeta,
) {
    let mut data = Vec::with_capacity(8 + struct_bytes.len());
    data.extend_from_slice(discriminator);
    data.extend_from_slice(struct_bytes);
    fs::write(dir.join(format!("{name}.bin")), &data).unwrap();

    let json = serde_json::to_string_pretty(meta).unwrap();
    fs::write(dir.join(format!("{name}.json")), json).unwrap();

    println!("wrote {name}.bin ({} bytes) and {name}.json", data.len());
}

fn main() {
    let fixtures_dir = Path::new(env!("CARGO_MANIFEST_DIR")).join("../fixtures");
    fs::create_dir_all(&fixtures_dir).unwrap();

    generate_program_config(&fixtures_dir);
    generate_distribution(&fixtures_dir);
    generate_journal(&fixtures_dir);
    generate_solana_validator_deposit(&fixtures_dir);
    generate_contributor_rewards(&fixtures_dir);

    println!("\nall fixtures generated in {}", fixtures_dir.display());
}

fn burn_rate(v: u32) -> BurnRate {
    BurnRate::new(v).unwrap()
}

fn validator_fee(v: u16) -> ValidatorFee {
    ValidatorFee::new(v).unwrap()
}

fn generate_program_config(dir: &Path) {
    let mut config = ProgramConfig::default();
    config.flags = U64::from(1);
    config.next_completed_dz_epoch = DoubleZeroEpoch::new(42);
    config.bump_seed = 253;
    config.reserve_2z_bump_seed = 252;
    config.swap_authority_bump_seed = 251;
    config.swap_destination_2z_bump_seed = 250;
    config.withdraw_sol_authority_bump_seed = 249;
    config.admin_key = pubkey_from_byte(1);
    config.debt_accountant_key = pubkey_from_byte(2);
    config.rewards_accountant_key = pubkey_from_byte(3);
    config.contributor_manager_key = pubkey_from_byte(4);
    config._placeholder_key = pubkey_from_byte(5);
    config.sol_2z_swap_program_id = pubkey_from_byte(6);
    config.distribution_parameters.calculation_grace_period_minutes = 120;
    config.distribution_parameters.initialization_grace_period_minutes = 60;
    config.distribution_parameters.minimum_epoch_duration_to_finalize_rewards = 3;
    config.distribution_parameters.community_burn_rate_parameters.limit = burn_rate(500_000_000);
    config
        .distribution_parameters
        .community_burn_rate_parameters
        .dz_epochs_to_increasing = 10;
    config
        .distribution_parameters
        .community_burn_rate_parameters
        .dz_epochs_to_limit = 100;
    config
        .distribution_parameters
        .solana_validator_fee_parameters
        .base_block_rewards_pct = validator_fee(500);
    config
        .distribution_parameters
        .solana_validator_fee_parameters
        .priority_block_rewards_pct = validator_fee(1000);
    config
        .distribution_parameters
        .solana_validator_fee_parameters
        .inflation_rewards_pct = validator_fee(200);
    config
        .distribution_parameters
        .solana_validator_fee_parameters
        .jito_tips_pct = validator_fee(300);
    config
        .distribution_parameters
        .solana_validator_fee_parameters
        .fixed_sol_amount = 50000;
    config.relay_parameters.distribute_rewards_lamports = 10000;
    config.last_initialized_distribution_timestamp = 1_700_000_000;
    config.debt_write_off_feature_activation_epoch = DoubleZeroEpoch::new(91);

    let disc = ProgramConfig::discriminator_slice();
    let meta = FixtureMeta {
        name: "ProgramConfig".into(),
        struct_size: std::mem::size_of::<ProgramConfig>(),
        discriminator_hex: hex_encode(disc),
        fields: vec![
            field_u64("Flags", 1),
            field_u64("NextCompletedDZEpoch", 42),
            field_u8("BumpSeed", 253),
            field_pubkey("AdminKey", &pubkey_from_byte(1)),
            field_pubkey("DebtAccountantKey", &pubkey_from_byte(2)),
            field_pubkey("RewardsAccountantKey", &pubkey_from_byte(3)),
            field_pubkey("ContributorManagerKey", &pubkey_from_byte(4)),
            field_pubkey("SOL2ZSwapProgramID", &pubkey_from_byte(6)),
            field_u16("CalculationGracePeriodMinutes", 120),
            field_u16("InitializationGracePeriodMinutes", 60),
            field_u8("MinimumEpochDurationToFinalizeRewards", 3),
            field_u32("BurnRateLimit", 500_000_000),
            field_u32("BurnRateDZEpochsToIncreasing", 10),
            field_u32("BurnRateDZEpochsToLimit", 100),
            field_u16("BaseBlockRewardsPct", 500),
            field_u16("PriorityBlockRewardsPct", 1000),
            field_u16("InflationRewardsPct", 200),
            field_u16("JitoTipsPct", 300),
            field_u32("FixedSOLAmount", 50000),
            field_u32("DistributeRewardsLamports", 10000),
            field_u64("DebtWriteOffFeatureActivationEpoch", 91),
        ],
    };

    write_fixture(dir, "program_config", disc, bytes_of(&config), &meta);
}

fn generate_distribution(dir: &Path) {
    let mut dist = Distribution::default();
    dist.dz_epoch = DoubleZeroEpoch::new(100);
    dist.flags = U64::from(7);
    dist.community_burn_rate = burn_rate(250_000_000);
    dist.bump_seed = 254;
    dist.token_2z_pda_bump_seed = 253;
    dist.solana_validator_fee_parameters.base_block_rewards_pct = validator_fee(500);
    dist.solana_validator_fee_parameters.priority_block_rewards_pct = validator_fee(1000);
    dist.solana_validator_fee_parameters.inflation_rewards_pct = validator_fee(200);
    dist.solana_validator_fee_parameters.jito_tips_pct = validator_fee(300);
    dist.solana_validator_fee_parameters.fixed_sol_amount = 50000;
    dist.total_solana_validators = 398;
    dist.solana_validator_payments_count = 350;
    dist.total_solana_validator_debt = 1_000_000_000;
    dist.collected_solana_validator_payments = 900_000_000;
    dist.total_contributors = 13;
    dist.distributed_rewards_count = 13;
    dist.collected_prepaid_2z_payments = 500_000;
    dist.collected_2z_converted_from_sol = 400_000;
    dist.uncollectible_sol_debt = 100_000;
    dist.distributed_2z_amount = 2_000_000;
    dist.burned_2z_amount = 1_500_000;
    dist.solana_validator_write_off_count = 5;

    let disc = Distribution::discriminator_slice();
    let meta = FixtureMeta {
        name: "Distribution".into(),
        struct_size: std::mem::size_of::<Distribution>(),
        discriminator_hex: hex_encode(disc),
        fields: vec![
            field_u64("DZEpoch", 100),
            field_u64("Flags", 7),
            field_u32("CommunityBurnRate", 250_000_000),
            field_u16("BaseBlockRewardsPct", 500),
            field_u16("PriorityBlockRewardsPct", 1000),
            field_u16("InflationRewardsPct", 200),
            field_u16("JitoTipsPct", 300),
            field_u32("FixedSOLAmount", 50000),
            field_u32("TotalSolanaValidators", 398),
            field_u32("SolanaValidatorPaymentsCount", 350),
            field_u64("TotalSolanaValidatorDebt", 1_000_000_000),
            field_u64("CollectedSolanaValidatorPayments", 900_000_000),
            field_u32("TotalContributors", 13),
            field_u32("DistributedRewardsCount", 13),
            field_u64("CollectedPrepaid2ZPayments", 500_000),
            field_u64("Collected2ZConvertedFromSOL", 400_000),
            field_u64("UncollectibleSOLDebt", 100_000),
            field_u64("Distributed2ZAmount", 2_000_000),
            field_u64("Burned2ZAmount", 1_500_000),
            field_u32("SolanaValidatorWriteOffCount", 5),
        ],
    };

    write_fixture(dir, "distribution", disc, bytes_of(&dist), &meta);
}

fn generate_journal(dir: &Path) {
    let mut journal = Journal::default();
    journal.bump_seed = 255;
    journal.token_2z_pda_bump_seed = 254;
    journal.total_sol_balance = 5_000_000_000;
    journal.total_2z_balance = 10_000_000;
    journal.swap_2z_destination_balance = 3_000_000;
    journal.swapped_sol_amount = 2_500_000_000;
    journal.next_dz_epoch_to_sweep_tokens = DoubleZeroEpoch::new(50);

    let disc = Journal::discriminator_slice();
    let meta = FixtureMeta {
        name: "Journal".into(),
        struct_size: std::mem::size_of::<Journal>(),
        discriminator_hex: hex_encode(disc),
        fields: vec![
            field_u8("BumpSeed", 255),
            field_u64("TotalSOLBalance", 5_000_000_000),
            field_u64("Total2ZBalance", 10_000_000),
            field_u64("Swap2ZDestinationBalance", 3_000_000),
            field_u64("SwappedSOLAmount", 2_500_000_000),
            field_u64("NextDZEpochToSweepTokens", 50),
        ],
    };

    write_fixture(dir, "journal", disc, bytes_of(&journal), &meta);
}

fn generate_solana_validator_deposit(dir: &Path) {
    let mut deposit = SolanaValidatorDeposit::default();
    deposit.node_id = pubkey_from_byte(42);
    deposit.written_off_sol_debt = 999_999;

    let disc = SolanaValidatorDeposit::discriminator_slice();
    let meta = FixtureMeta {
        name: "SolanaValidatorDeposit".into(),
        struct_size: std::mem::size_of::<SolanaValidatorDeposit>(),
        discriminator_hex: hex_encode(disc),
        fields: vec![
            field_pubkey("NodeID", &pubkey_from_byte(42)),
            field_u64("WrittenOffSOLDebt", 999_999),
        ],
    };

    write_fixture(dir, "solana_validator_deposit", disc, bytes_of(&deposit), &meta);
}

fn generate_contributor_rewards(dir: &Path) {
    let mut rewards = ContributorRewards::default();
    rewards.rewards_manager_key = pubkey_from_byte(10);
    rewards.service_key = pubkey_from_byte(11);
    rewards.flags = U64::from(1);

    let disc = ContributorRewards::discriminator_slice();
    let meta = FixtureMeta {
        name: "ContributorRewards".into(),
        struct_size: std::mem::size_of::<ContributorRewards>(),
        discriminator_hex: hex_encode(disc),
        fields: vec![
            field_pubkey("RewardsManagerKey", &pubkey_from_byte(10)),
            field_pubkey("ServiceKey", &pubkey_from_byte(11)),
            field_u64("Flags", 1),
        ],
    };

    write_fixture(dir, "contributor_rewards", disc, bytes_of(&rewards), &meta);
}

fn field_u8(name: &str, value: u8) -> FieldValue {
    FieldValue { name: name.into(), value: value.to_string(), typ: "u8".into() }
}

fn field_u16(name: &str, value: u16) -> FieldValue {
    FieldValue { name: name.into(), value: value.to_string(), typ: "u16".into() }
}

fn field_u32(name: &str, value: u32) -> FieldValue {
    FieldValue { name: name.into(), value: value.to_string(), typ: "u32".into() }
}

fn field_u64(name: &str, value: u64) -> FieldValue {
    FieldValue { name: name.into(), value: value.to_string(), typ: "u64".into() }
}

fn field_pubkey(name: &str, key: &Pubkey) -> FieldValue {
    FieldValue { name: name.into(), value: key.to_string(), typ: "pubkey".into() }
}

fn hex_encode(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}
