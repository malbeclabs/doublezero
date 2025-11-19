mod contributor_rewards;
mod convert_2z;
mod fetch;
mod harvest_2z;
mod relay;
mod validator_deposit;

//

use anyhow::{Context, Result, ensure};
use borsh::BorshDeserialize;
use clap::{Args, Subcommand};
use doublezero_contributor_rewards::calculator::proof::ShapleyOutputStorage;
use doublezero_revenue_distribution::{
    state::{Distribution, Journal, ProgramConfig, SolanaValidatorDeposit},
    types::{DoubleZeroEpoch, RewardShare},
};
use doublezero_sol_conversion_interface::{
    oracle::OraclePriceData,
    state::{
        ConfigurationRegistry as SolConversionConfigurationRegistry,
        ProgramState as SolConversionProgramState,
    },
};
use doublezero_solana_client_tools::{
    account::zero_copy::ZeroCopyAccountOwnedData,
    rpc::{DoubleZeroLedgerConnection, SolanaConnection},
};
use doublezero_solana_validator_debt::validator_debt::{
    ComputedSolanaValidatorDebt, ComputedSolanaValidatorDebts,
};
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::{pubkey::Pubkey, rent::Rent};

// TODO: Add testnet?
const SOL_2Z_ORACLE_ENDPOINT: &str =
    "https://sol-2z-oracle-api-v1.mainnet-beta.doublezero.xyz/swap-rate";

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

    /// Using the Revenue Distribution program's SOL liquidity, convert 2Z
    /// tokens to SOL. If there is not enough SOL liquidity for the
    /// fixed-quantity conversion, the command will fail.
    #[command(name = "convert-2z")]
    Convert2z(convert_2z::Convert2zCommand),

    #[command(name = "harvest-2z")]
    Harvest2z(harvest_2z::Harvest2zCommand),

    /// Manage a Solana validator deposit account. Funding can be directly with
    /// SOL or with 2Z limited by specified conversion rate for 2Z -> SOL.
    ValidatorDeposit(validator_deposit::ValidatorDepositCommand),

    /// Relayer instructions for the Revenue Distribution program.
    Relay(relay::RevenueDistributionRelayCommand),
}

impl RevenueDistributionSubcommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            Self::Fetch(command) => command.try_into_execute().await,
            Self::ContributorRewards(command) => command.try_into_execute().await,
            Self::Convert2z(command) => command.try_into_execute().await,
            Self::Harvest2z(command) => command.try_into_execute().await,
            Self::ValidatorDeposit(command) => command.try_into_execute().await,
            Self::Relay(command) => command.inner.try_into_execute().await,
        }
    }
}

//

async fn try_fetch_program_config(
    connection: &SolanaConnection,
) -> Result<(Pubkey, Box<ProgramConfig>)> {
    let (program_config_key, _) = ProgramConfig::find_address();

    let program_config = connection
        .try_fetch_zero_copy_data::<ProgramConfig>(&program_config_key)
        .await
        .context("Revenue Distribution program not initialized")?;
    Ok((program_config_key, program_config.mucked_data))
}

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

async fn try_fetch_distribution(
    connection: &SolanaConnection,
    dz_epoch: DoubleZeroEpoch,
) -> Result<(Pubkey, ZeroCopyAccountOwnedData<Distribution>)> {
    let (distribution_key, _) = Distribution::find_address(dz_epoch);

    let distribution = connection
        .try_fetch_zero_copy_data::<Distribution>(&distribution_key)
        .await
        .with_context(|| format!("Distribution not found for epoch {dz_epoch}"))?;
    Ok((distribution_key, distribution))
}

pub struct SolConversionState {
    pub program_state: (Pubkey, Box<SolConversionProgramState>),
    pub configuration_registry: (Pubkey, Box<SolConversionConfigurationRegistry>),
    pub journal: (Pubkey, ZeroCopyAccountOwnedData<Journal>),
    pub fixed_fill_quantity: u64,
}

impl SolConversionState {
    pub async fn try_fetch(rpc_client: &RpcClient) -> Result<Self> {
        const FAILED_FETCH_ERROR: &str = "SOL Conversion program not initialized";

        let (program_state_key, _) = SolConversionProgramState::find_address();
        let (configuration_registry_key, _) = SolConversionConfigurationRegistry::find_address();
        let (journal_key, _) = Journal::find_address();

        let account_infos = rpc_client
            .get_multiple_accounts(&[program_state_key, configuration_registry_key, journal_key])
            .await
            .context(FAILED_FETCH_ERROR)?
            .into_iter()
            .flatten()
            .collect::<Vec<_>>();
        ensure!(account_infos.len() == 3, FAILED_FETCH_ERROR);

        let program_state_data = Box::<_>::deserialize(&mut &account_infos[0].data[8..])?;
        let configuration_registry_data = Box::<SolConversionConfigurationRegistry>::deserialize(
            &mut &account_infos[1].data[8..],
        )?;

        let journal_data = ZeroCopyAccountOwnedData::from_account(&account_infos[2])
            .context("Revenue Distribution program not initialized")?;

        let fixed_fill_quantity = configuration_registry_data.fixed_fill_quantity;

        Ok(Self {
            program_state: (program_state_key, program_state_data),
            configuration_registry: (configuration_registry_key, configuration_registry_data),
            journal: (journal_key, journal_data),
            fixed_fill_quantity,
        })
    }
}

async fn try_request_oracle_conversion_price() -> Result<OraclePriceData> {
    reqwest::Client::new()
        .get(SOL_2Z_ORACLE_ENDPOINT)
        .header("User-Agent", "DoubleZero Solana CLI")
        .send()
        .await
        .with_context(|| format!("Failed to request SOL/2Z price from {SOL_2Z_ORACLE_ENDPOINT}"))?
        .json()
        .await
        .context("Failed to parse oracle response. Please try again")
}

fn try_is_processed_leaf(processed_leaf_data: &[u8], leaf_index: usize) -> Result<bool> {
    // Calculate which byte contains the bit for this leaf index
    // (8 bits per byte, so divide by 8)
    let leaf_byte_index = leaf_index / 8;

    // First, we have to grab the relevant byte from the processed data.
    // Create ByteFlags from the byte value to check the bit.
    let leaf_byte = processed_leaf_data
        .get(leaf_byte_index)
        .copied()
        .map(doublezero_revenue_distribution::types::ByteFlags::new)
        .with_context(|| format!("Invalid leaf index: {leaf_index}"))?;

    // Calculate which bit within the byte corresponds to this leaf
    // (modulo 8 gives us the bit position within the byte: 0-7).
    Ok(leaf_byte.bit(leaf_index % 8))
}

async fn try_fetch_shapley_record(
    dz_connection: &DoubleZeroLedgerConnection,
    rewards_accountant_key: &Pubkey,
    dz_epoch: DoubleZeroEpoch,
) -> Result<ShapleyOutputStorage> {
    const DEFAULT_SHAPLEY_OUTPUT_STORAGE_PREFIX: &[u8] = b"dz_contributor_rewards";

    doublezero_contributor_rewards::calculator::ledger_operations::try_fetch_shapley_output(
        dz_connection,
        DEFAULT_SHAPLEY_OUTPUT_STORAGE_PREFIX,
        rewards_accountant_key,
        dz_epoch.value(),
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
) -> Result<impl Iterator<Item = (usize, &'a ComputedSolanaValidatorDebt, bool)>> {
    let start_index = distribution.processed_solana_validator_debt_start_index as usize;
    let end_index = distribution.processed_solana_validator_debt_end_index as usize;
    let processed_leaf_data = &distribution.remaining_data[start_index..end_index];

    let num_debts = computed_debt.debts.len();
    let max_supported_debts = processed_leaf_data.len() * 8;

    ensure!(
        max_supported_debts >= num_debts,
        "Insufficient processed leaf data for epoch {}: can support {max_supported_debts} debts, but got {num_debts}",
        distribution.dz_epoch
    );

    Ok(computed_debt.debts.iter().enumerate().map(|(index, debt)| {
        let is_processed = try_is_processed_leaf(processed_leaf_data, index).unwrap();
        (index, debt, is_processed)
    }))
}
