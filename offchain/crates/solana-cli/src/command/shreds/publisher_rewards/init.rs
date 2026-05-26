use anyhow::{Result, bail};
use clap::Args;
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, TransactionOutcome, Wallet};
use doublezero_solana_sdk::{
    shred_subscription::{
        ID,
        instruction::{
            ShredSubscriptionInstructionData, account::InitializeValidatorPublisherRewardsAccounts,
        },
    },
    try_build_instruction,
};
use solana_sdk::{compute_budget::ComputeBudgetInstruction, pubkey::Pubkey};

/*
   doublezero-solana shreds publisher-rewards init --node-id <PUBKEY>
*/

#[derive(Debug, Args)]
pub struct InitCommand {
    /// Validator node identity. The seed for the validator publisher rewards PDA.
    #[arg(long)]
    pub node_id: Pubkey,

    #[command(flatten)]
    pub solana_payer_options: SolanaPayerOptions,
}

impl InitCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        if self.node_id == Pubkey::default() {
            bail!("--node-id must not be the default pubkey");
        }

        let dz_connection = self
            .solana_payer_options
            .connection_options
            .clone()
            .into_shred_subscription_connection();
        let wallet = Wallet::try_new(self.solana_payer_options, Some(dz_connection))?;
        let wallet_key = wallet.pubkey();

        println!("Shred subscription - Initialize Validator Publisher Rewards");
        println!("Node ID: {}", self.node_id);

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
            println!("Initialized validator publisher rewards: {tx_sig}");
            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}
