mod configure;
mod prepare_offchain_message;
mod rewards_mint_arg;
mod status;

use std::io::Write;

use anyhow::{Context, Result};
use clap::{Args, Subcommand};
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::{
    account::zero_copy::ZeroCopyAccountOwnedData, rpc::SolanaConnection,
};
use doublezero_solana_sdk::feed_subscription::state::{
    FeedPublishersRewardToken, find_feed_publishers_reward_token_address,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, Args)]
pub struct PublisherRewardsCommand {
    #[command(subcommand)]
    pub command: PublisherRewardsSubcommand,
}

impl PublisherRewardsCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        self.command.execute(ctx, out).await
    }
}

#[derive(Debug, Subcommand)]
pub enum PublisherRewardsSubcommand {
    /// Print the hex message the validator identity signs with `solana sign-offchain-message`.
    PrepareOffchainMessage(prepare_offchain_message::PrepareOffchainMessageCommand),
    /// Set the wallet and mint a publisher is paid in, creating its account when missing.
    Configure(configure::ConfigureCommand),
    /// Print where a publisher is paid and what it was paid in recent months.
    Status(status::StatusCommand),
}

impl PublisherRewardsSubcommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        match self {
            Self::PrepareOffchainMessage(command) => command.execute(ctx, out).await,
            Self::Configure(command) => command.execute(ctx, out).await,
            Self::Status(command) => command.execute(ctx, out).await,
        }
    }
}

/// Refuse a mint the feed program has no reward token for, before a transaction or an offline
/// signature is spent on it. A disabled mint passes with a warning, as the program accepts it.
async fn try_check_feed_publishers_reward_token(
    connection: &SolanaConnection,
    rewards_token_mint_key: &Pubkey,
) -> Result<()> {
    let reward_token_key = find_feed_publishers_reward_token_address(rewards_token_mint_key).0;
    let reward_token_account = connection
        .get_account_with_commitment(&reward_token_key, connection.commitment())
        .await
        .with_context(|| {
            format!(
                "reading the feed publishers reward token for mint {rewards_token_mint_key} (PDA {reward_token_key})"
            )
        })?
        .value
        .with_context(|| {
            format!(
                "the feed program does not pay publishers in mint {rewards_token_mint_key}, which has no feed publishers reward token"
            )
        })?;
    let reward_token =
        ZeroCopyAccountOwnedData::<FeedPublishersRewardToken>::from_account(&reward_token_account)
            .with_context(|| {
                format!("the feed publishers reward token at {reward_token_key} is malformed")
            })?;
    // On stderr, so `--json` output stays parseable.
    if !reward_token.is_enabled() {
        eprintln!(
            "warning: mint {rewards_token_mint_key} is disabled, so the feed program pays this publisher in 2Z until it configures an enabled mint"
        );
    }
    Ok(())
}
