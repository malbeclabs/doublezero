use anyhow::{Result, bail};
use clap::Args;
use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    ID,
    instruction::{
        RevenueDistributionInstructionData, account::InitializeContributorRewardsAccounts,
    },
    state::ContributorRewards,
};
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, Wallet};
use solana_sdk::{compute_budget::ComputeBudgetInstruction, pubkey::Pubkey};

#[derive(Debug, Args)]
pub struct ContributorRewardsCommand {
    service_key: Pubkey,

    #[arg(long)]
    initialize: bool,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,
}

impl ContributorRewardsCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let ContributorRewardsCommand {
            service_key,
            initialize,
            solana_payer_options,
        } = self;

        if !initialize {
            bail!("Nothing to do. Please specify `--initialize`");
        }

        let wallet = Wallet::try_from(solana_payer_options)?;
        let wallet_key = wallet.pubkey();

        let initialize_contributor_rewards_ix = try_build_instruction(
            &ID,
            InitializeContributorRewardsAccounts::new(&wallet_key, &service_key),
            &RevenueDistributionInstructionData::InitializeContributorRewards(service_key),
        )?;

        let mut compute_unit_limit = 10_000;

        let (_, bump) = ContributorRewards::find_address(&service_key);
        compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

        let mut instructions = vec![
            initialize_contributor_rewards_ix,
            ComputeBudgetInstruction::set_compute_unit_limit(compute_unit_limit),
        ];

        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

        if let Some(tx_sig) = tx_sig {
            println!("Initialized contributor rewards: {tx_sig}");

            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}
