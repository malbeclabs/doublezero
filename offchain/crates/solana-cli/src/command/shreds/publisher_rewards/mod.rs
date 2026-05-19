pub mod configure;
pub mod init;
pub mod prepare_offchain_message;
pub mod rewards_mint_arg;
pub mod show;

use anyhow::Result;
use clap::{Args, Subcommand};

#[derive(Debug, Args)]
pub struct PublisherRewardsCommand {
    #[command(subcommand)]
    pub command: PublisherRewardsSubcommand,
}

#[derive(Debug, Subcommand)]
pub enum PublisherRewardsSubcommand {
    /// Initialize the ValidatorPublisherRewards PDA (permissionless).
    Init(init::InitCommand),
    /// Print the hex blob to be signed via `solana sign-offchain-message`.
    PrepareOffchainMessage(prepare_offchain_message::PrepareOffchainMessageCommand),
    /// Configure the ValidatorPublisherRewards PDA (auto-inits if missing).
    Configure(configure::ConfigureCommand),
    /// Print current ValidatorPublisherRewards fields.
    Show(show::ShowCommand),
}

impl PublisherRewardsCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self.command {
            PublisherRewardsSubcommand::Init(c) => c.try_into_execute().await,
            PublisherRewardsSubcommand::PrepareOffchainMessage(c) => c.try_into_execute().await,
            PublisherRewardsSubcommand::Configure(c) => c.try_into_execute().await,
            PublisherRewardsSubcommand::Show(c) => c.try_into_execute().await,
        }
    }
}
