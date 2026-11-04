use std::io::Write;

use anyhow::{Context, Result, ensure};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::payer::{TransactionOutcome, Wallet};
use doublezero_solana_sdk::{
    revenue_distribution::{
        ID,
        instruction::{
            RevenueDistributionInstructionData,
            account::{
                InitializeSolanaValidatorDepositAccounts, WithdrawSolanaValidatorDepositAccounts,
            },
        },
        state::SolanaValidatorDeposit,
    },
    try_build_instruction,
};
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, Args)]
pub struct ValidatorDepositCommand {
    /// Node (Validator) identity.
    #[arg(long, short = 'n', value_name = "PUBKEY")]
    node_id: Pubkey,

    /// Initialize the Solana validator deposit account if it does not exist.
    #[arg(long, short = 'i')]
    initialize: bool,

    /// Fund the Solana validator deposit account with SOL.
    #[arg(long, value_name = "SOL")]
    fund: Option<String>,

    /// Withdraw excess balance from the Solana validator deposit account. This
    /// argument cannot be used with `--fund`.
    #[arg(long)]
    withdraw_excess_balance: bool,

    /// The public key of the account that will receive the excess balance. This
    /// argument is required when `--withdraw-excess-balance` is specified.
    ///
    /// NOTE: The keypair invoking the command must be the validator identity
    /// relevant to the deposit account.
    #[arg(long, value_name = "PUBKEY")]
    excess_balance_beneficiary: Option<Pubkey>,

    #[command(flatten)]
    write_opts: crate::command::WriteVerbOptions,
}

impl ValidatorDepositCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        let ValidatorDepositCommand {
            node_id,
            initialize: mut should_initialize,
            fund: fund_amount_str,
            withdraw_excess_balance: should_withdraw_excess_balance,
            excess_balance_beneficiary: excess_balance_beneficiary_key,
            write_opts,
        } = self;

        let wallet = crate::command::build_wallet(ctx, write_opts)?;
        let wallet_key = wallet.pubkey();

        ensure!(
            !(should_withdraw_excess_balance && fund_amount_str.is_some()),
            "Cannot use --withdraw-excess-balance and --fund together"
        );

        if should_withdraw_excess_balance {
            return try_withdraw_excess_balance(
                out,
                &wallet,
                &node_id,
                excess_balance_beneficiary_key.as_ref(),
            )
            .await;
        }

        // First check if the Solana validator deposit is already initialized.
        let (deposit_key, deposit, mut deposit_balance) =
            super::try_fetch_solana_validator_deposit(&wallet.connection, &node_id).await?;
        ensure!(
            !should_initialize || deposit.is_none(),
            "Solana validator deposit already initialized"
        );

        // Use the specified fund amount, if any.
        let (fund_lamports, memo_ix_and_compute_units) = if let Some(fund_str) = fund_amount_str {
            let fund_lamports = crate::utils::parse_sol_amount_to_lamports(fund_str)?;

            let (memo_ix, memo_compute_units) =
                Wallet::build_memo_instruction_with_compute_units(b"Funded");

            (fund_lamports, Some((memo_ix, memo_compute_units)))
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
            "Please specify `--initialize` or `--fund`"
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
            writeln!(out, "Solana validator deposit: {deposit_key}")?;
            if should_initialize {
                writeln!(out, "Funded and initialized: {tx_sig}")?;
            } else {
                writeln!(out, "Funded: {tx_sig}")?;
            }
            writeln!(out, "Node ID: {node_id}")?;
            writeln!(out, "Balance: {:.9} SOL", deposit_balance as f64 * 1e-9)?;

            wallet.write_verbose_output(out, &[tx_sig]).await?;
        }

        Ok(())
    }
}

async fn try_withdraw_excess_balance(
    out: &mut impl Write,
    wallet: &Wallet,
    node_id: &Pubkey,
    excess_balance_beneficiary_key: Option<&Pubkey>,
) -> Result<()> {
    ensure!(
        excess_balance_beneficiary_key.is_none() || wallet.pubkey() == *node_id,
        "The keypair invoking the command must be the validator identity relevant to the deposit account"
    );

    let (deposit_key, deposit, deposit_balance) =
        super::try_fetch_solana_validator_deposit(&wallet.connection, node_id).await?;

    let deposit =
        deposit.with_context(|| format!("No deposit account found for node ID {node_id}"))?;
    ensure!(
        deposit_balance.saturating_sub(deposit.written_off_sol_debt) > 0,
        "No excess balance found for node ID {node_id}"
    );

    let mut instructions = vec![
        try_build_instruction(
            &ID,
            WithdrawSolanaValidatorDepositAccounts::new(node_id, excess_balance_beneficiary_key),
            &RevenueDistributionInstructionData::WithdrawSolanaValidatorDeposit,
        )?,
        Wallet::build_memo_instruction(b"Withdrawn excess balance"),
        ComputeBudgetInstruction::set_compute_unit_limit(20_000),
    ];

    if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
        instructions.push(compute_unit_price_ix.clone());
    }

    let transaction = wallet.new_transaction(&instructions).await?;
    let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

    if let TransactionOutcome::Executed(tx_sig) = tx_sig {
        writeln!(out, "Solana validator deposit: {deposit_key}")?;
        writeln!(out, "Withdrawn excess balance: {tx_sig}")?;
        writeln!(out, "Node ID: {node_id}")?;
        writeln!(out, "Withdrawn {:.9} SOL", deposit_balance as f64 * 1e-9)?;
    }

    Ok(())
}
