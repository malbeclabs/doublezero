mod claim;
mod configure_proportion;

use std::io::Write;

use anyhow::{Result, ensure};
use clap::{Args, Subcommand};
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::payer::Wallet;
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
    /// Set a validator client's share of the rewards. Only the client's manager can sign it.
    ConfigureProportion(configure_proportion::ConfigureProportionCommand),
    /// Withdraw the 2Z in every open claim holding the client has. Only the client's manager can
    /// sign it.
    Claim(claim::ClaimCommand),
}

impl ValidatorClientRewardsSubcommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        match self {
            Self::ConfigureProportion(command) => command.execute(ctx, out).await,
            Self::Claim(command) => command.execute(ctx, out).await,
        }
    }
}

// Who signs for the manager: the wallet itself, or a Squads vault the multisig
// signs for once a payload is imported and approved.
enum Actor {
    Wallet(Box<Wallet>),
    Vault(Pubkey),
}

impl Actor {
    fn key(&self) -> Pubkey {
        match self {
            Self::Wallet(wallet) => wallet.pubkey(),
            Self::Vault(vault_key) => *vault_key,
        }
    }

    fn label(&self) -> &'static str {
        match self {
            Self::Wallet(_) => "wallet",
            Self::Vault(_) => "vault",
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
