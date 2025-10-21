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

use crate::command::{
    revenue_distribution::convert_2z::{self, Convert2zContext},
    try_prompt_proceed_confirmation,
};

#[derive(Debug, Args)]
pub struct ValidatorDepositCommand {
    /// Node (Validator) identity.
    #[arg(long, short = 'n', value_name = "PUBKEY")]
    node_id: Pubkey,

    /// Initialize the Solana validator deposit account if it does not exist.
    #[arg(long, short = 'i')]
    initialize: bool,

    /// Fund the Solana validator deposit account with SOL. When
    /// `--convert-2z-limit-price` is specified, the fund amount must match the
    /// required (fixed fill quantity) amount for the 2Z -> SOL conversion.
    #[arg(long, value_name = "SOL")]
    fund: Option<String>,

    /// Fund with 2Z limited by specified conversion rate for 2Z -> SOL.
    #[arg(long, value_name = "PRICE_LIMIT")]
    convert_2z_limit_price: Option<String>,

    /// Token account must be owned by the signer. Defaults to signer ATA if not
    /// specified.
    #[arg(long, value_name = "PUBKEY")]
    source_2z_account: Option<Pubkey>,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,
}

impl ValidatorDepositCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let ValidatorDepositCommand {
            node_id,
            initialize,
            fund,
            convert_2z_limit_price: convert_2z_limit_price_str,
            source_2z_account: source_2z_account_key,
            solana_payer_options,
        } = self;

        let wallet = Wallet::try_from(solana_payer_options)?;
        let wallet_key = wallet.pubkey();

        // First check if the Solana validator deposit is already initialized.
        let (deposit_key, deposit, mut deposit_balance) =
            super::fetch_solana_validator_deposit(&wallet.connection, &node_id).await;

        if initialize && deposit.is_some() {
            bail!("Solana validator deposit already initialized");
        }

        // Parse fund amount from SOL string (representing 9 decimal places at
        // most) to lamports.
        let fund_lamports = match fund {
            Some(fund) => crate::utils::parse_sol_amount_to_lamports(fund)?,
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

        struct Convert2zContextItems {
            user_token_account_key: Pubkey,
            token_balance_before: u64,
            required_lamports: u64,
        }

        let convert_2z_context = if let Some(limit_price_str) = convert_2z_limit_price_str {
            try_prompt_proceed_confirmation(
                format!(
                    "By specifying --convert-2z-limit-price, you are funding {:0.9} SOL to your deposit account",
                    fund_lamports as f64 * 1e-9,
                ),
                "Aborting command with --convert-2z-limit-price".to_string(),
            )?;

            let Convert2zContext {
                instruction,
                user_token_account_key,
                balance_before: token_balance_before,
                required_lamports,
            } = Convert2zContext::try_prepare(
                &wallet,
                Some(limit_price_str),
                source_2z_account_key,
                Some(fund_lamports),
            )
            .await?;
            println!(
                "2Z token balance: {:.8}",
                token_balance_before as f64 * 1e-8
            );

            instructions.push(instruction);
            compute_unit_limit += convert_2z::BUY_SOL_COMPUTE_UNIT_LIMIT;

            Some(Convert2zContextItems {
                user_token_account_key,
                token_balance_before,
                required_lamports,
            })
        } else {
            None
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

            if let Some(convert_2z_context) = convert_2z_context {
                let token_balance_after = convert_2z::fetch_token_balance(
                    &wallet,
                    Some(convert_2z_context.user_token_account_key),
                )
                .await?;
                println!(
                    "Converted {:.8} 2Z tokens to fund deposit with {:.9} SOL",
                    (convert_2z_context.token_balance_before - token_balance_after) as f64 * 1e-8,
                    (convert_2z_context.required_lamports as f64 * 1e-9)
                );
            }

            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}
