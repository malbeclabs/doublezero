mod claim;
mod init_holding;
mod set_proportion;
mod show;

use std::io::Write;

use anyhow::{Result, ensure};
use clap::{Args, Subcommand};
use doublezero_cli_core::CliContext;
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, Args)]
pub struct ValidatorClientRewardsCommand {
    #[command(subcommand)]
    pub command: ValidatorClientRewardsSubcommand,
}

impl ValidatorClientRewardsCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        self.command.execute(ctx, out).await
    }
}

#[derive(Debug, Subcommand)]
pub enum ValidatorClientRewardsSubcommand {
    /// Set the rewards proportion for a validator client.
    #[command(hide = true)]
    SetProportion(set_proportion::SetProportionCommand),
    /// Initialize one or more claim holding accounts (permissionless).
    InitHolding(init_holding::InitHoldingCommand),
    /// Drain N claim holdings into a destination token account.
    Claim(claim::ClaimCommand),
    /// Inspect a validator-client-rewards PDA and optional claim holdings.
    Show(show::ShowCommand),
}

impl ValidatorClientRewardsSubcommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        match self {
            Self::SetProportion(command) => command.execute(ctx, out).await,
            Self::InitHolding(command) => command.execute(ctx, out).await,
            Self::Claim(command) => command.execute(ctx, out).await,
            Self::Show(command) => command.execute(ctx, out).await,
        }
    }
}

/// Refuse an actor that is not the recorded manager. `actor` names what was
/// checked, the wallet or the vault, so the message says which key fell short.
fn validate_manager(
    actor: &str,
    actor_key: &Pubkey,
    validator_client_rewards_manager_key: &Pubkey,
) -> Result<()> {
    ensure!(
        actor_key == validator_client_rewards_manager_key,
        "manager mismatch: {actor} is {actor_key}, validator client rewards manager is {validator_client_rewards_manager_key}"
    );
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_validate_manager_matches() {
        let wallet_key = Pubkey::new_unique();
        assert!(validate_manager("wallet", &wallet_key, &wallet_key).is_ok());
    }

    #[test]
    fn test_validate_manager_mismatch_names_the_actor_checked() {
        let actor_key = Pubkey::new_unique();
        let manager_key = Pubkey::new_unique();
        for actor in ["wallet", "vault"] {
            let message = validate_manager(actor, &actor_key, &manager_key)
                .unwrap_err()
                .to_string();
            assert_eq!(
                message,
                format!(
                    "manager mismatch: {actor} is {actor_key}, validator client rewards manager \
                     is {manager_key}"
                )
            );
        }
    }
}
