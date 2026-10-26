use std::time::Duration;

use anyhow::{Context, Result, ensure};
use clap::Parser;
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, TransactionOutcome, Wallet};
use doublezero_solana_sdk::{
    environment_2z_token_mint_key,
    revenue_distribution::{
        ID,
        instruction::{
            RevenueDistributionInstructionData, account::InitializeDistributionAccounts,
        },
        state::{self, Distribution, ProgramConfig},
    },
    try_build_instruction,
};
use solana_compute_budget_interface::ComputeBudgetInstruction;

#[derive(Debug, Parser)]
struct CatchUpDistributions {
    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,

    /// Last DZ epoch to create a distribution for
    #[arg(long)]
    through_dz_epoch: u64,
}

#[tokio::main]
async fn main() -> Result<()> {
    let CatchUpDistributions {
        solana_payer_options,
        through_dz_epoch,
    } = CatchUpDistributions::parse();

    let wallet = Wallet::try_from(solana_payer_options)?;
    let wallet_key = wallet.pubkey();
    let network_env = wallet.connection.try_network_environment().await?;
    let dz_mint_key = environment_2z_token_mint_key(network_env);

    loop {
        let config = wallet
            .connection
            .try_fetch_zero_copy_data::<ProgramConfig>(&ProgramConfig::find_address().0)
            .await?;

        ensure!(
            !config.is_paused(),
            "The Revenue Distribution program is paused"
        );
        ensure!(
            wallet_key == config.debt_accountant_key,
            "Signer does not match expected debt accountant"
        );

        let next_dz_epoch = config.next_completed_dz_epoch;

        if next_dz_epoch.value() > through_dz_epoch {
            println!("Distributions exist through DZ epoch {through_dz_epoch}");
            return Ok(());
        }

        let grace_period_seconds = config
            .checked_distribution_initialization_grace_period_seconds()
            .context("Distribution initialization grace period not set")?;

        let (distribution_key, bump) = Distribution::find_address(next_dz_epoch);
        let (_, token_bump) = state::find_2z_token_pda_address(&distribution_key);
        let compute_unit_limit = 75_000
            + Wallet::compute_units_for_bump_seed(bump)
            + Wallet::compute_units_for_bump_seed(token_bump);

        let initialize_distribution_ix = try_build_instruction(
            &ID,
            InitializeDistributionAccounts::new(
                &wallet_key,
                &wallet_key,
                next_dz_epoch,
                &dz_mint_key,
            ),
            &RevenueDistributionInstructionData::InitializeDistribution,
        )?;

        let mut instructions = vec![
            initialize_distribution_ix,
            ComputeBudgetInstruction::set_compute_unit_limit(compute_unit_limit),
        ];

        if let Some(compute_unit_price_ix) = &wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;

        match wallet.send_or_simulate_transaction(&transaction).await? {
            TransactionOutcome::Executed(tx_sig) => {
                println!("Initialized distribution for DZ epoch {next_dz_epoch}: {tx_sig}");
            }
            TransactionOutcome::Simulated(_) => {
                // A simulation does not move the program to the next epoch.
                println!("Simulated the distribution for DZ epoch {next_dz_epoch}");
                return Ok(());
            }
        }

        // The program refuses another creation until the grace period passes.
        tokio::time::sleep(Duration::from_secs(u64::from(grace_period_seconds) + 2)).await;
    }
}
