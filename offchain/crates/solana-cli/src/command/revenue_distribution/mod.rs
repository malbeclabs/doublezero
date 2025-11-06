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
use doublezero_revenue_distribution::{
    state::{Distribution, Journal, ProgramConfig, SolanaValidatorDeposit},
    types::DoubleZeroEpoch,
};
use doublezero_sol_conversion_interface::{
    oracle::OraclePriceData,
    state::{
        ConfigurationRegistry as SolConversionConfigurationRegistry,
        ProgramState as SolConversionProgramState,
    },
};
use doublezero_solana_client_tools::{
    rpc::SolanaConnection,
    zero_copy::{ZeroCopyAccountOwned, ZeroCopyAccountOwnedData},
};
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::pubkey::Pubkey;

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

    let program_config =
        ZeroCopyAccountOwned::try_from_rpc_client(&connection.rpc_client, &program_config_key)
            .await
            .context("Revenue Distribution program not initialized")?;

    Ok((program_config_key, program_config.data.unwrap().mucked_data))
}

async fn fetch_solana_validator_deposit(
    connection: &SolanaConnection,
    node_id: &Pubkey,
) -> (
    Pubkey,
    Option<SolanaValidatorDeposit>,
    u64, // balance
) {
    let (solana_validator_deposit_key, _) = SolanaValidatorDeposit::find_address(node_id);

    match ZeroCopyAccountOwned::try_from_rpc_client(
        &connection.rpc_client,
        &solana_validator_deposit_key,
    )
    .await
    {
        Ok(solana_validator_deposit) => match solana_validator_deposit.data {
            Some(data) => (
                solana_validator_deposit_key,
                Some(*data.mucked_data),
                solana_validator_deposit.balance,
            ),
            None => (
                solana_validator_deposit_key,
                None,
                solana_validator_deposit.lamports,
            ),
        },
        Err(_) => (solana_validator_deposit_key, None, 0),
    }
}

async fn try_fetch_distribution(
    connection: &SolanaConnection,
    dz_epoch: DoubleZeroEpoch,
) -> Result<(Pubkey, ZeroCopyAccountOwnedData<Distribution>)> {
    let (distribution_key, _) = Distribution::find_address(dz_epoch);

    let failed_fetch_error = || format!("Distribution not found for epoch {dz_epoch}");

    let distribution =
        ZeroCopyAccountOwned::try_from_rpc_client(&connection.rpc_client, &distribution_key)
            .await
            .with_context(failed_fetch_error)?;

    let data = distribution.data.with_context(failed_fetch_error)?;
    Ok((distribution_key, data))
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

        let accounts = rpc_client
            .get_multiple_accounts(&[program_state_key, configuration_registry_key, journal_key])
            .await
            .context(FAILED_FETCH_ERROR)?;
        let account_datas = accounts
            .into_iter()
            .filter_map(|account| account.map(|account| account.data))
            .collect::<Vec<_>>();
        ensure!(account_datas.len() == 3, FAILED_FETCH_ERROR);

        let program_state_data = Box::<_>::deserialize(&mut &account_datas[0][8..])?;
        let configuration_registry_data =
            Box::<SolConversionConfigurationRegistry>::deserialize(&mut &account_datas[1][8..])?;

        let journal_data = ZeroCopyAccountOwnedData::new(&account_datas[2])
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

pub async fn try_request_oracle_conversion_price() -> Result<OraclePriceData> {
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
