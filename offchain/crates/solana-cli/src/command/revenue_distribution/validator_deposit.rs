use anyhow::{Result, bail};
use clap::Args;
use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    ID,
    instruction::{
        RevenueDistributionInstructionData, account::InitializeSolanaValidatorDepositAccounts,
    },
    state::SolanaValidatorDeposit,
};
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, Wallet};
use solana_sdk::{compute_budget::ComputeBudgetInstruction, pubkey::Pubkey};

#[derive(Debug, Args)]
pub struct ValidatorDepositCommand {
    node_id: Pubkey,

    #[arg(long)]
    initialize: bool,

    #[arg(long, value_name = "SOL")]
    fund: Option<String>,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,
}

impl ValidatorDepositCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let ValidatorDepositCommand {
            node_id,
            initialize,
            fund,
            solana_payer_options,
        } = self;

        let wallet = Wallet::try_from(solana_payer_options)?;
        let wallet_key = wallet.pubkey();

        // First check if the solana validator deposit is already initialized.
        let (deposit_key, deposit, mut deposit_balance) =
            super::fetch_solana_validator_deposit(&wallet.connection, &node_id).await;

        if initialize && deposit.is_some() {
            bail!("Solana validator deposit already initialized");
        }

        // Parse fund amount from SOL string (representing 9 decimal places at
        // most) to lamports.
        let fund_lamports = match fund {
            Some(fund) => parse_sol_to_lamports(fund)?,
            None => 0,
        };

        // Ensure that we initialize if it does not exist and we are funding.
        let should_initialize = deposit.is_none() && fund_lamports != 0;

        let mut instructions = vec![];
        let mut compute_unit_limit = 5_000;

        let and_initialized_str = if initialize || should_initialize {
            let initialize_solana_validator_deposit_ix = try_build_instruction(
                &ID,
                InitializeSolanaValidatorDepositAccounts::new(&wallet_key, &node_id),
                &RevenueDistributionInstructionData::InitializeSolanaValidatorDeposit(node_id),
            )?;

            instructions.push(initialize_solana_validator_deposit_ix);
            compute_unit_limit += 10_000;

            let (_, bump) = SolanaValidatorDeposit::find_address(&node_id);
            compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);

            " and initialized"
        } else {
            ""
        };

        if fund_lamports != 0 {
            deposit_balance += fund_lamports;

            let transfer_ix = solana_system_interface::instruction::transfer(
                &wallet_key,
                &deposit_key,
                fund_lamports,
            );
            instructions.push(transfer_ix);

            compute_unit_limit += 5_000;
        }

        if instructions.is_empty() {
            bail!("Nothing to do. Please specify `--initialize` or `--fund`");
        }

        instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
            compute_unit_limit,
        ));

        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

        if let Some(tx_sig) = tx_sig {
            println!("Solana validator deposit: {deposit_key}");
            println!("Funded{and_initialized_str}: {tx_sig}");
            println!("Node ID: {node_id}");
            println!("Balance: {:.9} SOL", deposit_balance as f64 * 1e-9);

            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}

//

fn parse_sol_to_lamports(sol_amount_str: String) -> Result<u64> {
    let sol_amount_str = sol_amount_str.trim();

    if sol_amount_str.is_empty() {
        bail!("SOL amount cannot be empty");
    }

    let sol_amount = sol_amount_str
        .parse::<f64>()
        .map_err(|_| anyhow::anyhow!("Invalid SOL amount: '{sol_amount_str}'"))?;

    if sol_amount <= 0.0 {
        bail!("SOL amount must be a positive value");
    }

    if sol_amount > (u64::MAX as f64 / 1e9) {
        bail!("SOL amount too large");
    }

    // Check that value is at most 9 decimal places.
    if let Some(decimal_index) = sol_amount_str.find('.') {
        let decimal_places = sol_amount_str.len() - decimal_index - 1;
        if decimal_places > 9 {
            bail!("SOL amount cannot have more than 9 decimal places");
        }
    }

    Ok((sol_amount * 1e9).round() as u64)
}
