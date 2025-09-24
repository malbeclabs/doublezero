mod contributor_rewards;
mod fetch;
mod relay;
mod solana_validator_deposit;

//

use anyhow::{Result, anyhow};
use clap::{Args, Subcommand};
use doublezero_revenue_distribution::state::{ProgramConfig, SolanaValidatorDeposit};
use doublezero_solana_client_tools::{rpc::SolanaConnection, zero_copy::ZeroCopyAccountOwned};
use solana_sdk::pubkey::Pubkey;

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

    /// Solana validator deposit account management.
    SolanaValidatorDeposit(solana_validator_deposit::SolanaValidatorDepositCommand),

    /// Relayer instructions for the Revenue Distribution program.
    Relay(relay::RevenueDistributionRelayCommand),
}

impl RevenueDistributionSubcommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            Self::Fetch(command) => command.try_into_execute().await,
            Self::ContributorRewards(command) => command.try_into_execute().await,
            Self::SolanaValidatorDeposit(command) => command.try_into_execute().await,
            Self::Relay(command) => command.inner.try_into_execute().await,
        }
    }
}

//

async fn try_fetch_program_config(
    connection: &SolanaConnection,
) -> Result<(Pubkey, ProgramConfig)> {
    let (program_config_key, _) = ProgramConfig::find_address();

    let program_config =
        ZeroCopyAccountOwned::from_rpc_client(&connection.rpc_client, &program_config_key)
            .await
            .map_err(|_| anyhow!("Revenue Distribution program not initialized"))?;

    Ok((program_config_key, program_config.data))
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

    match ZeroCopyAccountOwned::from_rpc_client(
        &connection.rpc_client,
        &solana_validator_deposit_key,
    )
    .await
    {
        Ok(solana_validator_deposit) => (
            solana_validator_deposit_key,
            Some(solana_validator_deposit.data),
            solana_validator_deposit.balance,
        ),
        Err(_) => (solana_validator_deposit_key, None, 0),
    }
}
