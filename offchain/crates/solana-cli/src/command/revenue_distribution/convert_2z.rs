use anyhow::{Context, Result, bail, ensure};
use clap::Args;
use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_sol_conversion_interface::{
    ID,
    instruction::{SolConversionInstructionData, account::BuySolAccounts},
    oracle::RATE_PRECISION,
};
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, Wallet};
use solana_sdk::{
    compute_budget::ComputeBudgetInstruction, instruction::Instruction, pubkey::Pubkey,
};

use crate::command::{
    revenue_distribution::{SolConversionState, try_request_oracle_conversion_price},
    try_prompt_proceed_confirmation,
};

#[derive(Debug, Args, Clone)]
pub struct Convert2zCommand {
    /// Limit price defaults to the current SOL/2Z oracle price.
    #[arg(long, value_name = "DECIMAL")]
    limit_price: Option<String>,

    /// Token account must be owned by the signer. Defaults to signer ATA if not
    /// specified.
    #[arg(long, value_name = "PUBKEY")]
    source_2z_account: Option<Pubkey>,

    /// Explicitly check SOL amount. When specified, this amount will be checked
    /// against the fixed fill quantity.
    #[arg(long, value_name = "SOL")]
    checked_sol_amount: Option<String>,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,
}

impl Convert2zCommand {
    pub const BUY_SOL_COMPUTE_UNIT_LIMIT: u32 = 80_000;

    pub async fn try_build_buy_sol_instruction(
        wallet: &Wallet,
        limit_price_str: Option<String>,
        source_token_account_key: Option<Pubkey>,
        checked_lamports: Option<u64>,
    ) -> Result<Instruction> {
        let wallet_key = wallet.pubkey();

        let dz_mint_key = doublezero_revenue_distribution::env::mainnet::DOUBLEZERO_MINT_KEY;

        let user_token_account_key = source_token_account_key.unwrap_or(
            spl_associated_token_account_interface::address::get_associated_token_address(
                &wallet_key,
                &dz_mint_key,
            ),
        );

        let SolConversionState {
            program_state: (_, sol_conversion_program_state),
            configuration_registry: (_, configuration_registry),
            journal: (_, journal, _),
        } = SolConversionState::try_fetch(&wallet.connection).await?;

        let required_lamports = configuration_registry.fixed_fill_quantity;
        ensure!(
            journal.total_sol_balance >= required_lamports,
            "Not enough SOL liquidity to cover conversion"
        );

        if let Some(specified_lamports) = checked_lamports {
            ensure!(
                specified_lamports == required_lamports,
                "SOL amount must be {:0.9} for 2Z -> SOL conversion. Got {:0.9}",
                required_lamports as f64 * 1e-9,
                specified_lamports as f64 * 1e-9,
            );
        }

        let oracle_price_data = try_request_oracle_conversion_price().await?;

        let limit_price = match limit_price_str {
            Some(limit_price_str) => parse_limit_price_to_u64(limit_price_str)?,
            None => oracle_price_data.swap_rate,
        };

        try_build_instruction(
            &ID,
            BuySolAccounts::new(
                &sol_conversion_program_state.fills_registry_key,
                &user_token_account_key,
                &dz_mint_key,
                &wallet_key,
            ),
            &SolConversionInstructionData::BuySol {
                limit_price,
                oracle_price_data,
            },
        )
        .context("Failed to build buy SOL instruction")
    }

    pub async fn try_into_execute(self) -> Result<()> {
        let Self {
            limit_price: limit_price_str,
            source_2z_account: source_token_account_key,
            checked_sol_amount: checked_sol_amount_str,
            solana_payer_options,
        } = self;

        let wallet = Wallet::try_from(solana_payer_options)?;

        let checked_lamports = match checked_sol_amount_str {
            Some(checked_sol_amount_str) => {
                let checked_lamports =
                    crate::utils::parse_sol_amount_to_lamports(checked_sol_amount_str)?;

                try_prompt_proceed_confirmation(
                    format!(
                        "You are converting 2Z to exactly {:0.9} SOL",
                        checked_lamports as f64 * 1e-9
                    ),
                    "Aborting command with --checked-sol-amount".to_string(),
                )?;

                Some(checked_lamports)
            }
            None => None,
        };

        let buy_sol_ix = Self::try_build_buy_sol_instruction(
            &wallet,
            limit_price_str,
            source_token_account_key,
            checked_lamports,
        )
        .await?;

        let mut instructions = vec![
            buy_sol_ix,
            ComputeBudgetInstruction::set_compute_unit_limit(Self::BUY_SOL_COMPUTE_UNIT_LIMIT),
        ];

        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

        if let Some(tx_sig) = tx_sig {
            println!("Buy SOL: {tx_sig}");
            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}

//

fn parse_limit_price_to_u64(bid_price_str: String) -> Result<u64> {
    const RATE_PRECISION_F64: f64 = RATE_PRECISION as f64;

    let bid_price_str = bid_price_str.trim();

    if bid_price_str.is_empty() {
        bail!("Bid price cannot be empty");
    }

    let bid_price = bid_price_str
        .parse::<f64>()
        .map_err(|_| anyhow::anyhow!("Invalid bid price: '{bid_price_str}'"))?;

    if bid_price <= 0.0 {
        bail!("Bid price must be a positive value");
    }

    if bid_price > (u64::MAX as f64 / RATE_PRECISION_F64) {
        bail!("Bid price too large");
    }

    // Check that value is at most 8 decimal places.
    if let Some(decimal_index) = bid_price_str.find('.') {
        let decimal_places = bid_price_str.len() - decimal_index - 1;
        if decimal_places > 8 {
            bail!("Bid price cannot have more than 8 decimal places");
        }
    }

    Ok((bid_price * RATE_PRECISION_F64).round() as u64)
}
