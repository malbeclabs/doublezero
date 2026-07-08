use std::io::Write;

use anyhow::{Context, Result, bail};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::payer::TransactionOutcome;
use doublezero_solana_sdk::{
    shred_subscription::{
        ID,
        instruction::{ShredSubscriptionInstructionData, account::InitializeClaimHoldingAccounts},
        state::{
            find_claim_holding_address, find_validator_client_rewards_address,
            parse_validator_client_rewards,
        },
    },
    try_build_instruction,
};
use solana_commitment_config::CommitmentConfig;
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_sdk::pubkey::Pubkey;

/*
   doublezero-solana shreds validator-client-rewards init-holding \
       --client-id <ID> --rewards-token-mint <PUBKEY> \
       --subscription-epoch <EPOCH> [--subscription-epoch <EPOCH> ...]
*/

/// Upper bound on epochs per init tx. Each init adds one instruction
/// (~22 bytes incl. accounts/data) plus two new holding/mint-token accounts
/// to the message; beyond ~20 the tx blows past the 1232-byte packet limit.
/// 16 leaves headroom for the CheckCliVersion ix and the fee-payer/system
/// account metas.
pub(crate) const MAX_INIT_HOLDING_EPOCHS_PER_TX: usize = 16;

#[derive(Debug, Args)]
pub struct InitHoldingCommand {
    /// Validator client ID.
    #[arg(long)]
    pub client_id: u16,
    /// Token mint that the holding account will hold.
    #[arg(long)]
    pub rewards_token_mint: Pubkey,
    /// One or more subscription epochs to initialize claim holding accounts for.
    #[arg(long = "subscription-epoch", required = true, num_args = 1..)]
    pub subscription_epochs: Vec<u64>,
    #[command(flatten)]
    pub write_opts: crate::command::WriteVerbOptions,
}

impl InitHoldingCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        if self.subscription_epochs.len() > MAX_INIT_HOLDING_EPOCHS_PER_TX {
            bail!(
                "too many --subscription-epoch values ({}); max {} per tx. Split into multiple `init-holding` calls.",
                self.subscription_epochs.len(),
                MAX_INIT_HOLDING_EPOCHS_PER_TX
            );
        }

        let wallet = crate::command::build_wallet(ctx, self.write_opts)?;
        let wallet_key = wallet.pubkey();

        let vcr_key = find_validator_client_rewards_address(self.client_id).0;

        // Verify VCR exists and has the right discriminator.
        let vcr_account = wallet
            .connection
            .get_account_with_commitment(&vcr_key, CommitmentConfig::confirmed())
            .await
            .with_context(|| format!("fetching VCR PDA {vcr_key}"))?
            .value;
        let vcr_data = match vcr_account {
            Some(acct) => acct.data,
            None => bail!(
                "validator client rewards not initialized for client-id {} (PDA {})",
                self.client_id,
                vcr_key
            ),
        };
        if parse_validator_client_rewards(&vcr_data).is_none() {
            bail!(
                "account at {vcr_key} is not a ValidatorClientRewards (unexpected discriminator or data layout)"
            );
        }

        // Pre-flight: filter epochs whose holding account already exists.
        let holding_keys: Vec<Pubkey> = self
            .subscription_epochs
            .iter()
            .map(|epoch| find_claim_holding_address(&vcr_key, *epoch, &self.rewards_token_mint).0)
            .collect();
        let holding_accounts = wallet
            .connection
            .get_multiple_accounts(&holding_keys)
            .await
            .with_context(|| "fetching claim holding accounts")?;

        let mut to_init: Vec<(u64, Pubkey)> = Vec::new();
        for ((epoch, key), maybe_acct) in self
            .subscription_epochs
            .iter()
            .zip(holding_keys.iter())
            .zip(holding_accounts.into_iter())
        {
            if maybe_acct.is_some() {
                writeln!(
                    out,
                    "epoch {epoch}: holding {key} already exists; skipping init"
                )?;
            } else {
                to_init.push((*epoch, *key));
            }
        }

        if to_init.is_empty() {
            writeln!(out, "All requested claim holdings already initialized.")?;
            return Ok(());
        }

        writeln!(
            out,
            "Shred subscription - Initialize Claim Holding Account (client_id={}, mint={}, epochs={})",
            self.client_id,
            self.rewards_token_mint,
            to_init
                .iter()
                .map(|(e, _)| e.to_string())
                .collect::<Vec<_>>()
                .join(",")
        )?;

        let mut instructions = vec![super::super::build_check_cli_version_instruction()?];
        for (epoch, _) in &to_init {
            let ix = try_build_instruction(
                &ID,
                InitializeClaimHoldingAccounts::new(
                    self.client_id,
                    *epoch,
                    &self.rewards_token_mint,
                    &wallet_key,
                ),
                &ShredSubscriptionInstructionData::InitializeClaimHolding(*epoch),
            )?;
            instructions.push(ix);
        }

        // Allow ~25k CU per init (system create + spl-token init + state update),
        // plus one for the check-cli-version ix.
        let cu_limit: u32 = 25_000u32.saturating_mul(to_init.len() as u32 + 1);
        instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(cu_limit));
        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

        if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
            writeln!(out, "Initialize claim holdings: {tx_sig}")?;
            for (epoch, key) in &to_init {
                writeln!(out, "  epoch {epoch}: {key}")?;
            }
            wallet.write_verbose_output(out, &[tx_sig]).await?;
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
        cmd: InitHoldingCommand,
    }

    #[test]
    fn parses_required_args() {
        let mint = Pubkey::new_unique();
        let cli = Cli::try_parse_from([
            "test",
            "--client-id",
            "7",
            "--rewards-token-mint",
            &mint.to_string(),
            "--subscription-epoch",
            "100",
        ])
        .unwrap();
        assert_eq!(cli.cmd.client_id, 7);
        assert_eq!(cli.cmd.rewards_token_mint, mint);
        assert_eq!(cli.cmd.subscription_epochs, vec![100u64]);
    }

    #[test]
    fn parses_multiple_subscription_epochs() {
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
            "--subscription-epoch",
            "102",
        ])
        .unwrap();
        assert_eq!(cli.cmd.subscription_epochs, vec![100u64, 101, 102]);
    }

    #[test]
    fn rejects_missing_client_id() {
        let mint = Pubkey::new_unique();
        let result = Cli::try_parse_from([
            "test",
            "--rewards-token-mint",
            &mint.to_string(),
            "--subscription-epoch",
            "100",
        ]);
        assert!(result.is_err());
    }

    #[test]
    fn rejects_missing_subscription_epoch() {
        let mint = Pubkey::new_unique();
        let result = Cli::try_parse_from([
            "test",
            "--client-id",
            "7",
            "--rewards-token-mint",
            &mint.to_string(),
        ]);
        assert!(result.is_err());
    }
}
