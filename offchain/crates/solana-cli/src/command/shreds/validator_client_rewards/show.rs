use std::io::Write;

use anyhow::{Context, Result, bail};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::{
    account::zero_copy::ZeroCopyAccountOwnedData, rpc::SolanaConnectionOptions,
};
use doublezero_solana_sdk::shred_subscription::state::{
    ValidatorClientRewards, find_claim_holding_address, find_validator_client_rewards_address,
};
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{program_pack::Pack, pubkey::Pubkey};
use spl_associated_token_account_interface::address::get_associated_token_address;

/*
   doublezero-solana shreds validator-client-rewards show \
       --client-id <ID> [--rewards-token-mint <PUBKEY>] \
       [--subscription-epoch <EPOCH> ...]
*/

#[derive(Debug, Args)]
pub struct ShowCommand {
    /// Validator client ID.
    #[arg(long)]
    pub client_id: u16,
    /// Filter to a specific token mint when listing holdings.
    #[arg(long)]
    pub rewards_token_mint: Option<Pubkey>,
    /// One or more subscription epochs to inspect. Requires --rewards-token-mint.
    #[arg(long = "subscription-epoch", num_args = 0..)]
    pub subscription_epochs: Vec<u64>,

    #[command(flatten)]
    connection_options: SolanaConnectionOptions,
}

pub(crate) fn render_validator_client_rewards_summary(
    validator_client_rewards_key: &Pubkey,
    validator_client_rewards: &ValidatorClientRewards,
) -> String {
    format!(
        "Validator client rewards (client_id={})\n  \
         PDA                 : {validator_client_rewards_key}\n  \
         manager             : {}\n  \
         description         : {}\n  \
         claim holding count : {}\n",
        validator_client_rewards.client_id,
        validator_client_rewards.manager_key,
        validator_client_rewards
            .checked_short_description()
            .unwrap_or("(none)"),
        validator_client_rewards.claim_holding_count,
    )
}

/// Status of a token-bearing account (manager ATA or per-epoch holding PDA)
/// for display purposes. Splits the cases that `Option<u64>` previously
/// collapsed so the user can tell apart "wasn't created" from "wrong owner /
/// malformed".
pub(crate) enum TokenAccountStatus {
    Balance(u64),
    DoesNotExist,
    WrongOwner(Pubkey),
    Malformed,
    WrongMint(Pubkey),
}

impl TokenAccountStatus {
    fn render_tail(&self, expected_mint: Option<&Pubkey>) -> String {
        match self {
            TokenAccountStatus::Balance(amt) => format!("balance={amt}"),
            TokenAccountStatus::DoesNotExist => "(does not exist)".to_string(),
            TokenAccountStatus::WrongOwner(owner) => format!("(wrong owner: {owner})"),
            TokenAccountStatus::Malformed => "(malformed token account)".to_string(),
            TokenAccountStatus::WrongMint(found) => match expected_mint {
                Some(expected) => {
                    format!("(wrong mint: found {found}, expected {expected})")
                }
                None => format!("(wrong mint: found {found})"),
            },
        }
    }
}

/// Classify a fetched token account against the expected mint (when known).
pub(crate) fn classify_token_account(
    account: Option<&solana_sdk::account::Account>,
    expected_mint: Option<&Pubkey>,
) -> TokenAccountStatus {
    let Some(account) = account else {
        return TokenAccountStatus::DoesNotExist;
    };
    if account.owner != spl_token_interface::ID {
        return TokenAccountStatus::WrongOwner(account.owner);
    }
    match spl_token_interface::state::Account::unpack(&account.data) {
        Err(_) => TokenAccountStatus::Malformed,
        Ok(token) => match expected_mint {
            Some(expected) if token.mint != *expected => TokenAccountStatus::WrongMint(token.mint),
            _ => TokenAccountStatus::Balance(token.amount),
        },
    }
}

// Format is grep'd by sh/test_doublezero_solana_fork.sh — keep
// "  epoch <num>  <pda>  balance=<n>" stable or update the grep.
pub(crate) fn render_holding_row(
    epoch: u64,
    holding_key: &Pubkey,
    status: &TokenAccountStatus,
    expected_mint: Option<&Pubkey>,
) -> String {
    format!(
        "  epoch {epoch:>5}  {holding_key}  {}",
        status.render_tail(expected_mint)
    )
}

pub(crate) fn render_manager_ata_row(
    ata: &Pubkey,
    status: &TokenAccountStatus,
    expected_mint: Option<&Pubkey>,
) -> String {
    format!(
        "  manager ATA  {ata}  {}",
        status.render_tail(expected_mint)
    )
}

impl ShowCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        if !self.subscription_epochs.is_empty() && self.rewards_token_mint.is_none() {
            bail!("--subscription-epoch requires --rewards-token-mint");
        }

        let connection = crate::command::solana_connection(ctx, &self.connection_options);

        let validator_client_rewards_key = find_validator_client_rewards_address(self.client_id).0;
        let validator_client_rewards_account = connection
            .get_account_with_commitment(
                &validator_client_rewards_key,
                CommitmentConfig::confirmed(),
            )
            .await
            .with_context(|| {
                format!("fetching validator client rewards PDA {validator_client_rewards_key}")
            })?
            .value;
        let Some(validator_client_rewards_account) = validator_client_rewards_account else {
            writeln!(
                out,
                "Validator client rewards not initialized for client-id {} (PDA {validator_client_rewards_key})",
                self.client_id
            )?;
            return Ok(());
        };
        let validator_client_rewards =
            ZeroCopyAccountOwnedData::<ValidatorClientRewards>::from_account(
                &validator_client_rewards_account,
            )
            .with_context(|| {
                format!("failed to decode ValidatorClientRewards at {validator_client_rewards_key}")
            })?;
        write!(
            out,
            "{}",
            render_validator_client_rewards_summary(
                &validator_client_rewards_key,
                &validator_client_rewards
            )
        )?;

        // When a mint is supplied, always print the manager's ATA address and
        // balance. Per-epoch holding rows are only listed when the user also
        // supplies one or more `--subscription-epoch` values.
        if let Some(mint) = self.rewards_token_mint {
            writeln!(out, "Claim holdings for mint {mint}:")?;
            let manager_ata_key =
                get_associated_token_address(&validator_client_rewards.manager_key, &mint);
            let ata_account = connection
                .get_account_with_commitment(&manager_ata_key, CommitmentConfig::confirmed())
                .await
                .with_context(|| format!("fetching manager ATA {manager_ata_key}"))?
                .value;
            let ata_status = classify_token_account(ata_account.as_ref(), Some(&mint));
            writeln!(
                out,
                "{}",
                render_manager_ata_row(&manager_ata_key, &ata_status, Some(&mint))
            )?;

            if !self.subscription_epochs.is_empty() {
                let holding_keys = self
                    .subscription_epochs
                    .iter()
                    .map(|e| find_claim_holding_address(&validator_client_rewards_key, *e, &mint).0)
                    .collect::<Vec<_>>();
                let holding_accounts = connection
                    .get_multiple_accounts(&holding_keys)
                    .await
                    .with_context(|| "fetching claim holdings")?;
                for ((epoch, key), maybe_acct) in self
                    .subscription_epochs
                    .iter()
                    .zip(holding_keys.iter())
                    .zip(holding_accounts.into_iter())
                {
                    let status = classify_token_account(maybe_acct.as_ref(), Some(&mint));
                    writeln!(
                        out,
                        "{}",
                        render_holding_row(*epoch, key, &status, Some(&mint))
                    )?;
                }
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use clap::Parser;

    use super::*;

    #[derive(Parser)]
    struct Cli {
        #[command(flatten)]
        cmd: ShowCommand,
    }

    #[test]
    fn parses_minimum_args() {
        let cli = Cli::try_parse_from(["test", "--client-id", "7"]).unwrap();
        assert_eq!(cli.cmd.client_id, 7);
        assert!(cli.cmd.rewards_token_mint.is_none());
        assert!(cli.cmd.subscription_epochs.is_empty());
    }

    #[test]
    fn parses_full_inspection_args() {
        let mint = Pubkey::new_unique();
        let cli = Cli::try_parse_from([
            "test",
            "--client-id",
            "7",
            "--rewards-token-mint",
            &mint.to_string(),
            "--subscription-epoch",
            "100",
            "--subscription-epoch",
            "101",
        ])
        .unwrap();
        assert_eq!(cli.cmd.rewards_token_mint, Some(mint));
        assert_eq!(cli.cmd.subscription_epochs, vec![100u64, 101]);
    }

    #[test]
    fn test_render_validator_client_rewards_summary_uses_none_when_description_empty() {
        let mut validator_client_rewards = ValidatorClientRewards::default();
        validator_client_rewards.client_id = 7;
        validator_client_rewards.manager_key = Pubkey::new_from_array([1; 32]);
        let validator_client_rewards_key = Pubkey::new_from_array([2; 32]);
        let out = render_validator_client_rewards_summary(
            &validator_client_rewards_key,
            &validator_client_rewards,
        );
        assert_eq!(
            out,
            [
                "Validator client rewards (client_id=7)",
                &format!("  PDA                 : {validator_client_rewards_key}"),
                &format!(
                    "  manager             : {}",
                    validator_client_rewards.manager_key
                ),
                "  description         : (none)",
                "  claim holding count : 0",
                "",
            ]
            .join("\n")
        );
    }

    #[test]
    fn test_render_validator_client_rewards_summary_renders_description() {
        let mut validator_client_rewards = ValidatorClientRewards::default();
        validator_client_rewards.client_id = 7;
        validator_client_rewards.manager_key = Pubkey::new_from_array([1; 32]);
        validator_client_rewards.short_description_bytes[..4].copy_from_slice(b"acme");
        validator_client_rewards.claim_holding_count = 4;
        let validator_client_rewards_key = Pubkey::new_from_array([2; 32]);
        let out = render_validator_client_rewards_summary(
            &validator_client_rewards_key,
            &validator_client_rewards,
        );
        assert!(out.contains("  description         : acme"));
        assert!(out.contains("  claim holding count : 4"));
    }

    #[test]
    fn render_holding_row_distinguishes_statuses() {
        let key = Pubkey::new_from_array([3u8; 32]);
        let expected_mint = Pubkey::new_from_array([5u8; 32]);
        let other_mint = Pubkey::new_from_array([6u8; 32]);
        let wrong_owner = Pubkey::new_from_array([7u8; 32]);

        let balance = render_holding_row(100, &key, &TokenAccountStatus::Balance(1_234_567), None);
        assert!(balance.contains("epoch   100"));
        assert!(balance.contains("balance=1234567"));

        let missing = render_holding_row(101, &key, &TokenAccountStatus::DoesNotExist, None);
        assert!(missing.contains("(does not exist)"));

        let bad_owner = render_holding_row(
            102,
            &key,
            &TokenAccountStatus::WrongOwner(wrong_owner),
            None,
        );
        assert!(bad_owner.contains("(wrong owner:"));
        assert!(bad_owner.contains(&wrong_owner.to_string()));

        let malformed = render_holding_row(103, &key, &TokenAccountStatus::Malformed, None);
        assert!(malformed.contains("(malformed token account)"));

        let wrong_mint = render_holding_row(
            104,
            &key,
            &TokenAccountStatus::WrongMint(other_mint),
            Some(&expected_mint),
        );
        assert!(wrong_mint.contains("(wrong mint: found"));
        assert!(wrong_mint.contains(&other_mint.to_string()));
        assert!(wrong_mint.contains(&expected_mint.to_string()));
    }

    #[test]
    fn render_manager_ata_row_distinguishes_statuses() {
        let ata = Pubkey::new_from_array([4u8; 32]);
        let present = render_manager_ata_row(&ata, &TokenAccountStatus::Balance(9_876_543), None);
        let missing = render_manager_ata_row(&ata, &TokenAccountStatus::DoesNotExist, None);
        let wrong_owner_key = Pubkey::new_from_array([8u8; 32]);
        let wrong_owner =
            render_manager_ata_row(&ata, &TokenAccountStatus::WrongOwner(wrong_owner_key), None);

        assert!(present.contains("manager ATA"));
        assert!(present.contains(&ata.to_string()));
        assert!(present.contains("balance=9876543"));

        assert!(missing.contains("manager ATA"));
        assert!(missing.contains(&ata.to_string()));
        assert!(missing.contains("(does not exist)"));

        assert!(wrong_owner.contains("(wrong owner:"));
        assert!(wrong_owner.contains(&wrong_owner_key.to_string()));
    }
}
