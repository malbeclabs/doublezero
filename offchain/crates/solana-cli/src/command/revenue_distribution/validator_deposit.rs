use anyhow::{Result, ensure};
use clap::Args;
use doublezero_solana_client_tools::{
    instruction::take_instruction,
    payer::{SolanaPayerOptions, TransactionOutcome, Wallet},
    rpc::{DoubleZeroLedgerEnvironmentOverride, SolanaConnection},
};
use doublezero_solana_sdk::{
    NetworkEnvironment, build_memo_instruction,
    revenue_distribution::{
        ID,
        instruction::{
            RevenueDistributionInstructionData, account::InitializeSolanaValidatorDepositAccounts,
        },
        state::SolanaValidatorDeposit,
        try_is_processed_leaf,
    },
    try_build_instruction,
};
use doublezero_solana_validator_debt::rpc::try_fetch_debt_records_and_distributions;
use solana_sdk::{compute_budget::ComputeBudgetInstruction, pubkey::Pubkey};

use crate::command::{
    revenue_distribution::{SolConversionState, convert_2z::Convert2zContext},
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

    /// Fund the Solana validator deposit account with outstanding debt. This
    /// argument cannot be used with `--fund`.
    #[arg(long)]
    fund_outstanding_debt: bool,

    /// Fund with 2Z limited by specified conversion rate for 2Z -> SOL.
    #[arg(long, value_name = "PRICE_LIMIT")]
    convert_2z_limit_price: Option<String>,

    /// Token account must be owned by the signer. Defaults to signer ATA if not
    /// specified.
    #[arg(long, value_name = "PUBKEY")]
    source_2z_account: Option<Pubkey>,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,

    #[arg(hide = true, long)]
    debt_accountant: Option<Pubkey>,

    #[command(flatten)]
    dz_env: DoubleZeroLedgerEnvironmentOverride,
}

impl ValidatorDepositCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let ValidatorDepositCommand {
            node_id,
            initialize: mut should_initialize,
            fund: fund_amount_str,
            fund_outstanding_debt: should_fund_outstanding_debt,
            convert_2z_limit_price: convert_2z_limit_price_str,
            source_2z_account: source_2z_account_key,
            solana_payer_options,
            debt_accountant: debt_accountant_key,
            dz_env,
        } = self;

        let wallet = Wallet::try_from(solana_payer_options)?;
        let wallet_key = wallet.pubkey();

        // First check if the Solana validator deposit is already initialized.
        let (deposit_key, deposit, mut deposit_balance) =
            super::try_fetch_solana_validator_deposit(&wallet.connection, &node_id).await?;
        ensure!(
            !should_initialize || deposit.is_none(),
            "Solana validator deposit already initialized"
        );

        // If specified, fund any outstanding debt. Otherwise, use the specified
        // fund amount.
        let (fund_lamports, memo_ix_and_compute_units) = if should_fund_outstanding_debt {
            ensure!(
                fund_amount_str.is_none(),
                "Cannot use --fund and --fund-outstanding-debt together"
            );

            let OutstandingDebt {
                amount: outstanding_debt_amount,
                last_solana_epoch,
            } = try_compute_outstanding_debt(
                &wallet.connection,
                &node_id,
                deposit_balance,
                dz_env.dz_env,
                debt_accountant_key.as_ref(),
            )
            .await?;

            if outstanding_debt_amount == 0 {
                println!("No outstanding debt found. Nothing to do");
                return Ok(());
            }

            let memo_ix = build_memo_instruction(
                format!("Funded through Solana epoch {last_solana_epoch}").as_bytes(),
            );

            (outstanding_debt_amount, Some((memo_ix, 15_000)))
        }
        // Parse fund amount from SOL string (representing 9 decimal places at
        // most) to lamports.
        else if let Some(fund_str) = fund_amount_str {
            let fund_lamports = crate::utils::parse_sol_amount_to_lamports(fund_str)?;

            let memo_ix = build_memo_instruction(b"Funded");

            (fund_lamports, Some((memo_ix, 5_000)))
        } else {
            Default::default()
        };

        // Ensure that we initialize if it does not exist and we are funding.
        should_initialize |= deposit.is_none() && fund_lamports != 0;

        let mut instructions = vec![];
        let mut compute_unit_limit = 5_000;

        if should_initialize {
            let initialize_solana_validator_deposit_ix = try_build_instruction(
                &ID,
                InitializeSolanaValidatorDepositAccounts::new(&wallet_key, &node_id),
                &RevenueDistributionInstructionData::InitializeSolanaValidatorDeposit(node_id),
            )?;

            instructions.push(initialize_solana_validator_deposit_ix);
            compute_unit_limit += 10_000;

            let (_, bump) = SolanaValidatorDeposit::find_address(&node_id);
            compute_unit_limit += Wallet::compute_units_for_bump_seed(bump);
        };

        struct Convert2zContextItems {
            context: Convert2zContext,
            token_balance_before: u64,
            required_lamports: u64,
        }

        let convert_2z_context_items = if let Some(limit_price_str) = convert_2z_limit_price_str {
            try_prompt_proceed_confirmation(
                format!(
                    "By specifying --convert-2z-limit-price, you are funding {:0.9} SOL to your deposit account",
                    fund_lamports as f64 * 1e-9,
                ),
                "Aborting command with --convert-2z-limit-price".to_string(),
            )?;

            let sol_conversion_state = SolConversionState::try_fetch(&wallet.connection).await?;

            let mut convert_2z_context = Convert2zContext::try_prepare(
                &wallet,
                &sol_conversion_state,
                Some(limit_price_str),
                source_2z_account_key,
                Some(fund_lamports),
            )
            .await?;
            let buy_sol_ix = take_instruction(&mut convert_2z_context.instruction);

            let token_balance_before = convert_2z_context
                .try_token_balance(&wallet.connection)
                .await?;
            println!(
                "2Z token balance: {:.8}",
                token_balance_before as f64 * 1e-8
            );

            instructions.push(buy_sol_ix);
            compute_unit_limit += Convert2zContext::BUY_SOL_COMPUTE_UNIT_LIMIT;

            Some(Convert2zContextItems {
                context: convert_2z_context,
                token_balance_before,
                required_lamports: sol_conversion_state.fixed_fill_quantity,
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

        ensure!(
            !instructions.is_empty(),
            "Please specify `--initialize`, `--fund-outstanding-debt` or `--fund`"
        );

        if let Some((memo_ix, memo_compute_units)) = memo_ix_and_compute_units {
            instructions.push(memo_ix);
            compute_unit_limit += memo_compute_units;
        }

        instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
            compute_unit_limit,
        ));

        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

        // TODO: Add simulation result handling with state changes.
        if let TransactionOutcome::Executed(tx_sig) = tx_sig {
            println!("Solana validator deposit: {deposit_key}");
            if should_initialize {
                println!("Funded and initialized: {tx_sig}");
            } else {
                println!("Funded: {tx_sig}");
            }
            println!("Node ID: {node_id}");
            println!("Balance: {:.9} SOL", deposit_balance as f64 * 1e-9);

            if let Some(Convert2zContextItems {
                context: convert_2z_context,
                token_balance_before,
                required_lamports,
            }) = convert_2z_context_items
            {
                let token_balance_after = convert_2z_context
                    .try_token_balance(&wallet.connection)
                    .await?;
                println!(
                    "Converted {:.8} 2Z tokens to fund deposit with {:.9} SOL",
                    (token_balance_before - token_balance_after) as f64 * 1e-8,
                    (required_lamports as f64 * 1e-9)
                );
            }

            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}

struct OutstandingDebt {
    amount: u64,
    last_solana_epoch: u64,
}

async fn try_compute_outstanding_debt(
    solana_connection: &SolanaConnection,
    node_id: &Pubkey,
    deposit_balance: u64,
    dz_env_override: Option<NetworkEnvironment>,
    debt_accountant_key: Option<&Pubkey>,
) -> Result<OutstandingDebt> {
    let debt_records_and_distributions = try_fetch_debt_records_and_distributions(
        solana_connection,
        dz_env_override,
        debt_accountant_key,
    )
    .await?;

    let mut total_debt = 0;
    let mut last_solana_epoch = 0;

    for (debt_record, distribution) in debt_records_and_distributions {
        if debt_record.debts.is_empty() {
            continue;
        }

        let index = debt_record
            .data
            .debts
            .iter()
            .position(|debt| &debt.node_id == node_id);

        if let Some(index) = index {
            let processed_range = distribution.processed_solana_validator_debt_bitmap_range();
            let processed_leaf_data = &distribution.remaining_data[processed_range];

            if try_is_processed_leaf(processed_leaf_data, index).unwrap() {
                continue;
            }

            total_debt += debt_record.data.debts[index].amount;
            last_solana_epoch = debt_record.data.last_solana_epoch;
        }
    }

    Ok(OutstandingDebt {
        amount: total_debt.saturating_sub(deposit_balance),
        last_solana_epoch,
    })
}
