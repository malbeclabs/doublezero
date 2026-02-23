use std::str::FromStr;

use anyhow::{Result, bail, ensure};
use clap::Args;
use doublezero_solana_client_tools::{
    account::zero_copy::ZeroCopyAccountOwnedData,
    payer::{SolanaPayerOptions, TransactionOutcome, Wallet},
};
use doublezero_solana_sdk::{
    revenue_distribution::{
        ID,
        instruction::{
            ContributorRewardsConfiguration, RevenueDistributionInstructionData,
            account::ConfigureContributorRewardsAccounts,
        },
        state::{ContributorRewards, MAX_RECIPIENTS},
    },
    try_build_instruction,
};
use solana_sdk::{
    compute_budget::ComputeBudgetInstruction, instruction::Instruction, pubkey::Pubkey,
};

pub const BASIS_POINTS_SCALE: u16 = 10_000;
pub const HUMAN_PERCENT_SCALE: u8 = 100;

/// Configure a contributor rewards account.
///
/// This command allows setting recipient shares and/or controlling whether
/// protocol management can change the rewards manager.
#[derive(Debug, Args)]
#[command(name = "configure-contributor-rewards")]
pub struct ConfigureContributorRewardsCommand {
    /// The service key that identifies the ContributorRewards account (PDA seed).
    #[arg(long, value_name = "PUBKEY")]
    service_key: Pubkey,

    /// Recipient share in the format PUBKEY:PERCENT (1-100, integer).
    /// Can be specified multiple times. Maximum 8 recipients.
    /// All percentages must sum to exactly 100.
    ///
    /// Example: --recipient HFishy...:30 --recipient 7xKXt...:70
    #[arg(long = "recipient", value_name = "PUBKEY:PERCENT")]
    recipients: Vec<String>,

    /// Block protocol management from changing the rewards manager.
    /// Mutually exclusive with --allow-protocol-management.
    #[arg(long, conflicts_with = "allow_protocol_management")]
    block_protocol_management: bool,

    /// Allow protocol management to change the rewards manager.
    /// Mutually exclusive with --block-protocol-management.
    #[arg(long, conflicts_with = "block_protocol_management")]
    allow_protocol_management: bool,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,
}

#[derive(Debug, Clone)]
pub struct ConfigureContributorRewardsArgs {
    /// The service key that identifies the ContributorRewards account.
    pub service_key: Pubkey,
    /// The wallet/signer pubkey (rewards manager).
    pub signer_key: Pubkey,
    /// Parsed recipients as (Pubkey, basis_points).
    pub recipients: Vec<(Pubkey, u16)>,
    /// Whether to block protocol management.
    pub block_protocol_management: bool,
    /// Whether to allow protocol management.
    pub allow_protocol_management: bool,
    /// Optional compute unit price instruction.
    pub compute_unit_price_ix: Option<Instruction>,
}

#[derive(Debug)]
pub struct ConfigureContributorRewardsInstructions {
    /// The full list of instructions, with ComputeBudget instructions first.
    pub instructions: Vec<Instruction>,
    /// The computed unit limit used (exposed for testing/logging).
    #[allow(dead_code)]
    pub compute_unit_limit: u32,
}

pub fn build_configure_contributor_rewards_instructions(
    args: &ConfigureContributorRewardsArgs,
) -> Result<ConfigureContributorRewardsInstructions> {
    let ConfigureContributorRewardsArgs {
        service_key,
        signer_key,
        recipients,
        block_protocol_management,
        allow_protocol_management,
        compute_unit_price_ix,
    } = args;

    let has_recipients = !recipients.is_empty();
    let has_block_flag = *block_protocol_management || *allow_protocol_management;

    if !has_recipients && !has_block_flag {
        bail!(
            "Nothing to configure. Provide at least one --recipient \
             or use --block-protocol-management / --allow-protocol-management"
        );
    }

    if has_recipients {
        validate_recipients(recipients)?;
    }

    let mut program_instructions = Vec::new();
    let mut compute_unit_limit = 5_000u32;

    let (_, bump) = ContributorRewards::find_address(service_key);
    compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

    if has_recipients {
        let recipients_ix = try_build_instruction(
            &ID,
            ConfigureContributorRewardsAccounts::new(signer_key, service_key),
            &RevenueDistributionInstructionData::ConfigureContributorRewards(
                ContributorRewardsConfiguration::Recipients(recipients.clone()),
            ),
        )?;
        program_instructions.push(recipients_ix);
        compute_unit_limit += 10_000;
    }

    if *block_protocol_management {
        let block_ix = try_build_instruction(
            &ID,
            ConfigureContributorRewardsAccounts::new(signer_key, service_key),
            &RevenueDistributionInstructionData::ConfigureContributorRewards(
                ContributorRewardsConfiguration::IsSetRewardsManagerBlocked(true),
            ),
        )?;
        program_instructions.push(block_ix);
        compute_unit_limit += 5_000;
    } else if *allow_protocol_management {
        let allow_ix = try_build_instruction(
            &ID,
            ConfigureContributorRewardsAccounts::new(signer_key, service_key),
            &RevenueDistributionInstructionData::ConfigureContributorRewards(
                ContributorRewardsConfiguration::IsSetRewardsManagerBlocked(false),
            ),
        )?;
        program_instructions.push(allow_ix);
        compute_unit_limit += 5_000;
    }

    let mut instructions = Vec::with_capacity(program_instructions.len() + 2);

    instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
        compute_unit_limit,
    ));

    if let Some(price_ix) = compute_unit_price_ix {
        instructions.push(price_ix.clone());
    }

    instructions.extend(program_instructions);

    Ok(ConfigureContributorRewardsInstructions {
        instructions,
        compute_unit_limit,
    })
}

impl ConfigureContributorRewardsCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let ConfigureContributorRewardsCommand {
            service_key,
            recipients,
            block_protocol_management,
            allow_protocol_management,
            solana_payer_options,
        } = self;

        // Parse recipients from CLI strings (human percent -> basis points).
        let parsed_recipients = parse_recipients(&recipients)?;

        let wallet = Wallet::try_from(solana_payer_options)?;
        let wallet_key = wallet.pubkey();

        // Preflight check: verify the signer is the rewards manager.
        preflight_check_rewards_manager(&wallet, &service_key, &wallet_key).await?;

        // Build instructions.
        let args = ConfigureContributorRewardsArgs {
            service_key,
            signer_key: wallet_key,
            recipients: parsed_recipients,
            block_protocol_management,
            allow_protocol_management,
            compute_unit_price_ix: wallet.compute_unit_price_ix.clone(),
        };

        let result = build_configure_contributor_rewards_instructions(&args)?;

        let transaction = wallet.new_transaction(&result.instructions).await?;
        let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

        if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
            println!("Configured contributor rewards: {tx_sig}");
            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}

/// Preflight check: fetch the ContributorRewards account and verify the signer
/// is the current rewards_manager_key.
///
/// This provides a clear local error before sending a transaction that would fail.
async fn preflight_check_rewards_manager(
    wallet: &Wallet,
    service_key: &Pubkey,
    signer_key: &Pubkey,
) -> Result<()> {
    let (pda_key, _) = ContributorRewards::find_address(service_key);

    let contributor_rewards: ZeroCopyAccountOwnedData<ContributorRewards> =
        match wallet.connection.try_fetch_zero_copy_data(&pda_key).await {
            Ok(data) => data,
            Err(_) => {
                return Ok(());
            }
        };

    // Check if signer matches the stored rewards_manager_key.
    if contributor_rewards.rewards_manager_key != *signer_key {
        bail!(
            "Signer {} is not the rewards manager for this ContributorRewards account.\n\
             Current rewards manager: {}\n\
             ContributorRewards PDA: {}",
            signer_key,
            contributor_rewards.rewards_manager_key,
            pda_key
        );
    }

    Ok(())
}

/// Parse a single recipient string in the format "PUBKEY:PERCENT".
///
/// PERCENT is human-readable (1-100), converted to basis points (100-10000) internally.
/// Returns (Pubkey, u16) where u16 is the share in basis points.
fn parse_recipient(s: &str) -> Result<(Pubkey, u16)> {
    let parts: Vec<&str> = s.split(':').collect();

    ensure!(
        parts.len() == 2,
        "Invalid recipient format: '{}'. Expected PUBKEY:PERCENT (e.g., HFishy...:30)",
        s
    );

    let pubkey_str = parts[0];
    let percent_str = parts[1];

    let pubkey = Pubkey::from_str(pubkey_str).map_err(|e| {
        anyhow::anyhow!(
            "Invalid pubkey '{}' in recipient '{}': {}",
            pubkey_str,
            s,
            e
        )
    })?;

    ensure!(
        pubkey != Pubkey::default(),
        "Invalid recipient: zero pubkey is not allowed"
    );

    let percent: u8 = percent_str.parse().map_err(|e| {
        anyhow::anyhow!(
            "Invalid percentage '{}' in recipient '{}': {}. Must be an integer 1-100",
            percent_str,
            s,
            e
        )
    })?;

    ensure!(
        percent > 0,
        "Invalid percentage {} in recipient '{}': must be greater than 0",
        percent,
        s
    );
    ensure!(
        percent <= HUMAN_PERCENT_SCALE,
        "Invalid percentage {} in recipient '{}': must be at most {} (100%)",
        percent,
        s,
        HUMAN_PERCENT_SCALE
    );

    let basis_points = u16::from(percent) * (BASIS_POINTS_SCALE / u16::from(HUMAN_PERCENT_SCALE));

    Ok((pubkey, basis_points))
}

fn parse_recipients(recipients: &[String]) -> Result<Vec<(Pubkey, u16)>> {
    recipients.iter().map(|s| parse_recipient(s)).collect()
}

fn validate_recipients(recipients: &[(Pubkey, u16)]) -> Result<()> {
    ensure!(
        recipients.len() <= MAX_RECIPIENTS,
        "Too many recipients: {} provided, maximum is {}",
        recipients.len(),
        MAX_RECIPIENTS
    );

    let mut seen = std::collections::HashSet::new();
    for (pubkey, _) in recipients {
        ensure!(
            seen.insert(*pubkey),
            "Duplicate recipient pubkey: {}",
            pubkey
        );
    }

    let total: u32 = recipients.iter().map(|(_, share)| u32::from(*share)).sum();
    ensure!(
        total == u32::from(BASIS_POINTS_SCALE),
        "Recipient percentages must sum to 100% (got {}%)",
        total / 100
    );

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    // A valid non-zero pubkey for testing (Token program).
    const TEST_PUBKEY: &str = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA";
    // Another valid pubkey for testing (Associated Token program).
    const TEST_PUBKEY_2: &str = "ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL";

    #[test]
    fn test_parse_recipient_valid() {
        let input = format!("{}:50", TEST_PUBKEY);
        let result = parse_recipient(&input);
        assert!(result.is_ok());
        let (pubkey, bps) = result.unwrap();
        assert_eq!(pubkey, Pubkey::from_str(TEST_PUBKEY).unwrap());
        assert_eq!(bps, 5000);
    }

    #[test]
    fn test_parse_recipient_max_percentage() {
        let input = format!("{}:100", TEST_PUBKEY);
        let result = parse_recipient(&input);
        assert!(result.is_ok());
        assert_eq!(result.unwrap().1, 10000);
    }

    #[test]
    fn test_parse_recipient_small_percentage() {
        let input = format!("{}:1", TEST_PUBKEY);
        let result = parse_recipient(&input);
        assert!(result.is_ok());
        assert_eq!(result.unwrap().1, 100);
    }

    #[test]
    fn test_parse_recipient_missing_colon() {
        let input = format!("{}50", TEST_PUBKEY);
        let result = parse_recipient(&input);
        assert!(result.is_err());
        assert!(
            result
                .unwrap_err()
                .to_string()
                .contains("Expected PUBKEY:PERCENT")
        );
    }

    #[test]
    fn test_parse_recipient_extra_colon() {
        let input = format!("{}:50:00", TEST_PUBKEY);
        let result = parse_recipient(&input);
        assert!(result.is_err());
        assert!(
            result
                .unwrap_err()
                .to_string()
                .contains("Expected PUBKEY:PERCENT")
        );
    }

    #[test]
    fn test_parse_recipient_invalid_pubkey() {
        let input = "not_a_valid_pubkey:50";
        let result = parse_recipient(input);
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("Invalid pubkey"));
    }

    #[test]
    fn test_parse_recipient_non_numeric_percentage() {
        let input = format!("{}:abc", TEST_PUBKEY);
        let result = parse_recipient(&input);
        assert!(result.is_err());
        assert!(
            result
                .unwrap_err()
                .to_string()
                .contains("Invalid percentage")
        );
    }

    #[test]
    fn test_parse_recipient_zero_percentage() {
        let input = format!("{}:0", TEST_PUBKEY);
        let result = parse_recipient(&input);
        assert!(result.is_err());
        assert!(
            result
                .unwrap_err()
                .to_string()
                .contains("must be greater than 0")
        );
    }

    #[test]
    fn test_parse_recipient_percentage_too_high() {
        let input = format!("{}:101", TEST_PUBKEY);
        let result = parse_recipient(&input);
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("must be at most"));
    }

    #[test]
    fn test_parse_recipient_zero_pubkey() {
        let input = "11111111111111111111111111111111:50";
        let result = parse_recipient(input);
        assert!(result.is_err());
        assert!(
            result
                .unwrap_err()
                .to_string()
                .contains("zero pubkey is not allowed")
        );
    }

    #[test]
    fn test_validate_recipients_valid() {
        let recipients = vec![(Pubkey::new_unique(), 3000), (Pubkey::new_unique(), 7000)];
        assert!(validate_recipients(&recipients).is_ok());
    }

    #[test]
    fn test_validate_recipients_sum_not_100() {
        let recipients = vec![(Pubkey::new_unique(), 3000), (Pubkey::new_unique(), 5000)];
        let result = validate_recipients(&recipients);
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("sum to 100%"));
    }

    #[test]
    fn test_validate_recipients_duplicates() {
        let key = Pubkey::new_unique();
        let recipients = vec![(key, 5000), (key, 5000)];
        let result = validate_recipients(&recipients);
        assert!(result.is_err());
        assert!(
            result
                .unwrap_err()
                .to_string()
                .contains("Duplicate recipient")
        );
    }

    #[test]
    fn test_validate_recipients_max_exceeded() {
        let recipients: Vec<_> = (0..9).map(|_| (Pubkey::new_unique(), 1111)).collect();
        let result = validate_recipients(&recipients);
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("maximum is 8"));
    }

    #[test]
    fn test_validate_recipients_exactly_8() {
        let recipients: Vec<_> = (0..8).map(|_| (Pubkey::new_unique(), 1250)).collect();
        assert!(validate_recipients(&recipients).is_ok());
    }

    #[test]
    fn test_validate_recipients_single_recipient_100_percent() {
        let recipients = vec![(Pubkey::new_unique(), 10000)];
        assert!(validate_recipients(&recipients).is_ok());
    }

    #[test]
    fn test_validate_recipients_empty() {
        let recipients: Vec<(Pubkey, u16)> = vec![];
        let result = validate_recipients(&recipients);
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("sum to 100%"));
    }

    #[test]
    fn test_build_instructions_nothing_to_do() {
        let args = ConfigureContributorRewardsArgs {
            service_key: Pubkey::new_unique(),
            signer_key: Pubkey::new_unique(),
            recipients: vec![],
            block_protocol_management: false,
            allow_protocol_management: false,
            compute_unit_price_ix: None,
        };
        let result = build_configure_contributor_rewards_instructions(&args);
        assert!(result.is_err());
        assert!(
            result
                .unwrap_err()
                .to_string()
                .contains("Nothing to configure")
        );
    }

    #[test]
    fn test_build_instructions_recipients_only() {
        let args = ConfigureContributorRewardsArgs {
            service_key: Pubkey::new_unique(),
            signer_key: Pubkey::new_unique(),
            recipients: vec![(Pubkey::new_unique(), 10000)],
            block_protocol_management: false,
            allow_protocol_management: false,
            compute_unit_price_ix: None,
        };
        let result = build_configure_contributor_rewards_instructions(&args).unwrap();

        assert_eq!(result.instructions.len(), 2);

        assert_eq!(
            result.instructions[0].program_id,
            solana_sdk::compute_budget::id()
        );
    }

    #[test]
    fn test_build_instructions_block_flag_only() {
        let args = ConfigureContributorRewardsArgs {
            service_key: Pubkey::new_unique(),
            signer_key: Pubkey::new_unique(),
            recipients: vec![],
            block_protocol_management: true,
            allow_protocol_management: false,
            compute_unit_price_ix: None,
        };
        let result = build_configure_contributor_rewards_instructions(&args).unwrap();

        assert_eq!(result.instructions.len(), 2);

        assert_eq!(
            result.instructions[0].program_id,
            solana_sdk::compute_budget::id()
        );
    }

    #[test]
    fn test_build_instructions_allow_flag_only() {
        let args = ConfigureContributorRewardsArgs {
            service_key: Pubkey::new_unique(),
            signer_key: Pubkey::new_unique(),
            recipients: vec![],
            block_protocol_management: false,
            allow_protocol_management: true,
            compute_unit_price_ix: None,
        };
        let result = build_configure_contributor_rewards_instructions(&args).unwrap();

        assert_eq!(result.instructions.len(), 2);
    }

    #[test]
    fn test_build_instructions_with_priority_fee() {
        let priority_ix = ComputeBudgetInstruction::set_compute_unit_price(1000);

        let args = ConfigureContributorRewardsArgs {
            service_key: Pubkey::new_unique(),
            signer_key: Pubkey::new_unique(),
            recipients: vec![(Pubkey::new_unique(), 10000)],
            block_protocol_management: false,
            allow_protocol_management: false,
            compute_unit_price_ix: Some(priority_ix),
        };
        let result = build_configure_contributor_rewards_instructions(&args).unwrap();

        assert_eq!(result.instructions.len(), 3);

        assert_eq!(
            result.instructions[0].program_id,
            solana_sdk::compute_budget::id()
        );
        assert_eq!(
            result.instructions[1].program_id,
            solana_sdk::compute_budget::id()
        );
    }

    #[test]
    fn test_build_instructions_compute_budget_first() {
        let priority_ix = ComputeBudgetInstruction::set_compute_unit_price(1000);

        let args = ConfigureContributorRewardsArgs {
            service_key: Pubkey::new_unique(),
            signer_key: Pubkey::new_unique(),
            recipients: vec![(Pubkey::new_unique(), 5000), (Pubkey::new_unique(), 5000)],
            block_protocol_management: true,
            allow_protocol_management: false,
            compute_unit_price_ix: Some(priority_ix),
        };
        let result = build_configure_contributor_rewards_instructions(&args).unwrap();

        assert_eq!(result.instructions.len(), 4);

        assert_eq!(
            result.instructions[0].program_id,
            solana_sdk::compute_budget::id(),
            "First instruction must be ComputeBudget"
        );
        assert_eq!(
            result.instructions[1].program_id,
            solana_sdk::compute_budget::id(),
            "Second instruction must be ComputeBudget"
        );

        assert_eq!(result.instructions[2].program_id, ID);
        assert_eq!(result.instructions[3].program_id, ID);
    }

    #[test]
    fn test_build_instructions_no_block_flag_means_no_block_ix() {
        let args = ConfigureContributorRewardsArgs {
            service_key: Pubkey::new_unique(),
            signer_key: Pubkey::new_unique(),
            recipients: vec![(Pubkey::new_unique(), 10000)],
            block_protocol_management: false,
            allow_protocol_management: false,
            compute_unit_price_ix: None,
        };
        let result = build_configure_contributor_rewards_instructions(&args).unwrap();

        assert_eq!(result.instructions.len(), 2);

        let program_ixs: Vec<_> = result
            .instructions
            .iter()
            .filter(|ix| ix.program_id == ID)
            .collect();
        assert_eq!(
            program_ixs.len(),
            1,
            "Should have exactly 1 program instruction"
        );
    }

    #[test]
    fn test_build_instructions_block_true_adds_block_ix() {
        let args = ConfigureContributorRewardsArgs {
            service_key: Pubkey::new_unique(),
            signer_key: Pubkey::new_unique(),
            recipients: vec![],
            block_protocol_management: true,
            allow_protocol_management: false,
            compute_unit_price_ix: None,
        };
        let result = build_configure_contributor_rewards_instructions(&args).unwrap();

        assert_eq!(result.instructions.len(), 2);

        let program_ixs: Vec<_> = result
            .instructions
            .iter()
            .filter(|ix| ix.program_id == ID)
            .collect();
        assert_eq!(program_ixs.len(), 1);
    }

    #[test]
    fn test_build_instructions_allow_true_adds_allow_ix() {
        let args = ConfigureContributorRewardsArgs {
            service_key: Pubkey::new_unique(),
            signer_key: Pubkey::new_unique(),
            recipients: vec![],
            block_protocol_management: false,
            allow_protocol_management: true,
            compute_unit_price_ix: None,
        };
        let result = build_configure_contributor_rewards_instructions(&args).unwrap();

        assert_eq!(result.instructions.len(), 2);

        let program_ixs: Vec<_> = result
            .instructions
            .iter()
            .filter(|ix| ix.program_id == ID)
            .collect();
        assert_eq!(program_ixs.len(), 1);
    }

    #[test]
    fn test_build_instructions_recipients_and_block() {
        let args = ConfigureContributorRewardsArgs {
            service_key: Pubkey::new_unique(),
            signer_key: Pubkey::new_unique(),
            recipients: vec![(Pubkey::new_unique(), 10000)],
            block_protocol_management: true,
            allow_protocol_management: false,
            compute_unit_price_ix: None,
        };
        let result = build_configure_contributor_rewards_instructions(&args).unwrap();

        assert_eq!(result.instructions.len(), 3);

        let program_ixs: Vec<_> = result
            .instructions
            .iter()
            .filter(|ix| ix.program_id == ID)
            .collect();
        assert_eq!(program_ixs.len(), 2);
    }

    #[test]
    fn test_parse_and_validate_30_70_split() {
        let inputs = vec![
            format!("{}:30", TEST_PUBKEY),
            format!("{}:70", TEST_PUBKEY_2),
        ];
        let parsed = parse_recipients(&inputs).unwrap();

        assert_eq!(parsed[0].1, 3000);
        assert_eq!(parsed[1].1, 7000);

        assert!(validate_recipients(&parsed).is_ok());
    }

    #[test]
    fn test_parse_and_validate_equal_split() {
        let inputs = vec![
            format!("{}:50", TEST_PUBKEY),
            format!("{}:50", TEST_PUBKEY_2),
        ];
        let parsed = parse_recipients(&inputs).unwrap();
        assert!(validate_recipients(&parsed).is_ok());
    }

    #[test]
    fn test_parse_and_validate_uneven_split_fails() {
        let inputs = vec![
            format!("{}:30", TEST_PUBKEY),
            format!("{}:60", TEST_PUBKEY_2),
        ];
        let parsed = parse_recipients(&inputs).unwrap();
        let result = validate_recipients(&parsed);
        assert!(result.is_err());
    }
}
