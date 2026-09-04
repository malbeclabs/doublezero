mod configure_contributor_rewards;
mod contributor_rewards;
mod fetch;
mod relay;
mod validator_deposit;

//

use std::io::Write;

use anyhow::{Result, ensure};
use clap::{Args, Subcommand};
use doublezero_cli_core::CliContext;
use doublezero_contributor_rewards::calculator::proof::ShapleyOutputStorage;
use doublezero_solana_client_tools::{
    account::zero_copy::ZeroCopyAccountOwnedData,
    rpc::{DoubleZeroLedgerConnection, SolanaConnection},
};
use doublezero_solana_sdk::revenue_distribution::{
    state::{Distribution, SolanaValidatorDeposit},
    try_is_processed_leaf,
    types::RewardShare,
};
use doublezero_solana_validator_debt::validator_debt::{
    ComputedSolanaValidatorDebt, ComputedSolanaValidatorDebts,
};
use solana_sdk::{pubkey::Pubkey, rent::Rent};

#[derive(Debug, Args)]
pub struct RevenueDistributionCommand {
    #[command(subcommand)]
    pub command: RevenueDistributionSubcommand,
}

#[derive(Debug, Subcommand)]
pub enum RevenueDistributionSubcommand {
    /// Fetch accounts associated with the Revenue Distribution program.
    Fetch(fetch::FetchCommand),

    /// Contributor rewards account management.
    ContributorRewards(contributor_rewards::ContributorRewardsCommand),

    /// Configure a contributor rewards account: set recipient shares and/or
    /// control whether protocol management can change the rewards manager.
    #[command(name = "configure-contributor-rewards")]
    ConfigureContributorRewards(configure_contributor_rewards::ConfigureContributorRewardsCommand),

    /// Manage a Solana validator deposit account.
    ValidatorDeposit(validator_deposit::ValidatorDepositCommand),

    /// Relayer instructions for the Revenue Distribution program.
    Relay(relay::RevenueDistributionRelayCommand),
}

impl RevenueDistributionSubcommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        match self {
            Self::Fetch(command) => command.execute(ctx, out).await,
            Self::ContributorRewards(command) => command.execute(ctx, out).await,
            Self::ConfigureContributorRewards(command) => command.execute(ctx, out).await,
            Self::ValidatorDeposit(command) => command.execute(ctx, out).await,
            Self::Relay(command) => command.inner.execute(ctx, out).await,
        }
    }
}

//

async fn try_fetch_solana_validator_deposit(
    connection: &SolanaConnection,
    node_id: &Pubkey,
) -> Result<(
    Pubkey,
    Option<SolanaValidatorDeposit>,
    u64, // balance
)> {
    let (solana_validator_deposit_key, _) = SolanaValidatorDeposit::find_address(node_id);

    match connection
        .get_multiple_accounts(&[solana_validator_deposit_key, solana_sdk::sysvar::rent::ID])
        .await
    {
        Ok(account_infos) => {
            let account_infos = account_infos
                .into_iter()
                .map(Option::unwrap_or_default)
                .collect::<Vec<_>>();

            let solana_validator_deposit_info = &account_infos[0];
            let rent_sysvar =
                solana_sdk::account::from_account::<Rent, _>(&account_infos[1]).unwrap();

            let balance = doublezero_solana_client_tools::account::balance(
                solana_validator_deposit_info,
                &rent_sysvar,
            );

            let solana_validator_deposit =
                ZeroCopyAccountOwnedData::<SolanaValidatorDeposit>::from_account(
                    solana_validator_deposit_info,
                );

            match solana_validator_deposit {
                Some(data) => Ok((
                    solana_validator_deposit_key,
                    Some(*data.mucked_data),
                    balance,
                )),
                None => Ok((solana_validator_deposit_key, None, balance)),
            }
        }
        Err(_) => Ok((solana_validator_deposit_key, None, 0)),
    }
}

async fn try_fetch_shapley_record(
    dz_connection: &DoubleZeroLedgerConnection,
    rewards_accountant_key: &Pubkey,
    dz_epoch_value: u64,
) -> Result<ShapleyOutputStorage> {
    const DEFAULT_SHAPLEY_OUTPUT_STORAGE_PREFIX: &[u8] = b"dz_contributor_rewards";

    doublezero_contributor_rewards::calculator::ledger_operations::try_fetch_shapley_output(
        dz_connection,
        DEFAULT_SHAPLEY_OUTPUT_STORAGE_PREFIX,
        rewards_accountant_key,
        dz_epoch_value,
    )
    .await
}

fn try_distribution_rewards_iter<'a>(
    distribution: &ZeroCopyAccountOwnedData<Distribution>,
    shapley_output: &'a ShapleyOutputStorage,
) -> Result<impl Iterator<Item = (usize, &'a RewardShare, bool)>> {
    let start_index = distribution.processed_rewards_start_index as usize;
    let end_index = distribution.processed_rewards_end_index as usize;
    let processed_leaf_data = &distribution.remaining_data[start_index..end_index];

    let num_rewards = shapley_output.rewards.len();
    let max_supported_rewards = processed_leaf_data.len() * 8;

    ensure!(
        max_supported_rewards >= num_rewards,
        "Insufficient processed leaf data for epoch {}: can support {max_supported_rewards} rewards, but got {num_rewards}",
        distribution.dz_epoch
    );

    Ok(shapley_output
        .rewards
        .iter()
        .enumerate()
        .map(|(index, reward_share)| {
            let is_processed = try_is_processed_leaf(processed_leaf_data, index).unwrap();
            (index, reward_share, is_processed)
        }))
}

fn try_distribution_solana_validator_debt_iter<'a>(
    distribution: &ZeroCopyAccountOwnedData<Distribution>,
    computed_debt: &'a ComputedSolanaValidatorDebts,
) -> Result<
    impl Iterator<
        Item = (
            usize,
            &'a ComputedSolanaValidatorDebt,
            bool, // is_processed_leaf
            bool, // is_written_off_leaf
        ),
    >,
> {
    let start_index = distribution.processed_solana_validator_debt_start_index as usize;
    let end_index = distribution.processed_solana_validator_debt_end_index as usize;
    let processed_leaf_data = &distribution.remaining_data[start_index..end_index];

    let num_debts = computed_debt.debts.len();
    let max_supported_debts = processed_leaf_data.len() * 8;

    let written_off_leaf_data = if distribution.is_solana_validator_debt_write_off_enabled() {
        let start_index =
            distribution.processed_solana_validator_debt_write_off_start_index as usize;
        let end_index = distribution.processed_solana_validator_debt_write_off_end_index as usize;
        Some(&distribution.remaining_data[start_index..end_index])
    } else {
        None
    };

    ensure!(
        max_supported_debts >= num_debts,
        "Insufficient processed leaf data for epoch {}: can support {max_supported_debts} debts, but got {num_debts}",
        distribution.dz_epoch
    );

    Ok(computed_debt
        .debts
        .iter()
        .enumerate()
        .map(move |(index, debt)| {
            let is_processed = try_is_processed_leaf(processed_leaf_data, index).unwrap();
            let is_written_off = written_off_leaf_data
                .map(|data| try_is_processed_leaf(data, index).unwrap())
                .unwrap_or(false);
            (index, debt, is_processed, is_written_off)
        }))
}
