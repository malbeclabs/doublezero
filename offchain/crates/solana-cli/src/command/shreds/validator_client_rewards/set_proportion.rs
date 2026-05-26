use anyhow::{Result, bail};
use clap::Args;
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, TransactionOutcome, Wallet};
use doublezero_solana_sdk::{
    shred_subscription::{
        ID,
        instruction::{
            ShredSubscriptionInstructionData, account::SetValidatorClientRewardsProportionAccounts,
        },
    },
    try_build_instruction,
};
use solana_sdk::compute_budget::ComputeBudgetInstruction;

/*
   doublezero-solana shreds validator-client-rewards set-proportion \
       --client-id <ID> --proportion <PERCENT>
*/

#[derive(Debug, Args)]
pub struct SetProportionCommand {
    /// Validator client ID.
    #[arg(long)]
    client_id: u16,
    /// Rewards proportion as a percentage (0–100, e.g. 50.5 for 50.5%).
    #[arg(long)]
    proportion: f64,
    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,
}

impl SetProportionCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let proportion_bps = percentage_to_bps(self.proportion)?;

        let dz_connection = self
            .solana_payer_options
            .connection_options
            .clone()
            .into_shred_subscription_connection();
        let wallet = Wallet::try_new(self.solana_payer_options, Some(dz_connection))?;
        let wallet_key = wallet.pubkey();

        println!("Shred subscription - Set Validator Client Rewards Proportion");
        println!(
            "Client ID: {}, Proportion: {}% ({} bps)",
            self.client_id, self.proportion, proportion_bps
        );

        let ix = try_build_instruction(
            &ID,
            SetValidatorClientRewardsProportionAccounts::new(&wallet_key, self.client_id),
            &ShredSubscriptionInstructionData::SetValidatorClientRewardsProportion(proportion_bps),
        )?;

        let check_ix = super::super::build_check_cli_version_instruction()?;
        let mut instructions = vec![check_ix, ix];

        instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(35_000));
        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

        if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
            println!("Set validator client rewards proportion: {tx_sig}");
            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}

fn percentage_to_bps(pct: f64) -> Result<u16> {
    if !(0.0..=100.0).contains(&pct) {
        bail!("Proportion must be between 0 and 100 (got {pct})");
    }
    Ok((pct * 100.0).round() as u16)
}
