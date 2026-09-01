use std::io::Write;

use anyhow::{Result, bail};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::payer::TransactionOutcome;
use doublezero_solana_sdk::{
    shred_subscription::{
        ID,
        instruction::{
            ShredSubscriptionInstructionData, account::InitializeValidatorPublisherRewardsAccounts,
        },
    },
    try_build_instruction,
};
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_sdk::pubkey::Pubkey;

/*
   doublezero-solana shreds publisher-rewards init --node-id <PUBKEY>
*/

#[derive(Debug, Args)]
pub struct InitCommand {
    /// Validator node identity. The seed for the validator publisher rewards PDA.
    #[arg(long)]
    pub node_id: Pubkey,

    #[command(flatten)]
    pub write_opts: crate::command::WriteVerbOptions,
}

impl InitCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        if self.node_id == Pubkey::default() {
            bail!("--node-id must not be the default pubkey");
        }

        let wallet = crate::command::build_wallet(ctx, self.write_opts)?;
        let wallet_key = wallet.pubkey();

        writeln!(
            out,
            "Shred subscription - Initialize Validator Publisher Rewards"
        )?;
        writeln!(out, "Node ID: {}", self.node_id)?;

        let ix = try_build_instruction(
            &ID,
            InitializeValidatorPublisherRewardsAccounts::new(&wallet_key, &self.node_id),
            &ShredSubscriptionInstructionData::InitializeValidatorPublisherRewards(self.node_id),
        )?;

        let check_ix = super::super::build_check_cli_version_instruction()?;
        let mut instructions = vec![check_ix, ix];

        instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(20_000));
        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

        if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
            writeln!(out, "Initialized validator publisher rewards: {tx_sig}")?;
            wallet.write_verbose_output(out, &[tx_sig]).await?;
        }

        Ok(())
    }
}
