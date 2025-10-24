use anyhow::{Context, Result, ensure};
use clap::Args;
use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::env::mainnet::DOUBLEZERO_MINT_KEY;
use doublezero_sol_conversion_interface::{
    ID,
    instruction::{SolConversionInstructionData, account::BuySolAccounts},
};
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, Wallet};
use solana_sdk::{
    compute_budget::ComputeBudgetInstruction, instruction::Instruction, program_pack::Pack,
    pubkey::Pubkey,
};

use crate::command::{
    revenue_distribution::{SolConversionState, try_request_oracle_conversion_price},
    try_prompt_proceed_confirmation,
};

#[derive(Debug, Args, Clone)]
pub struct Convert2zCommand {
    /// Limit price defaults to the current SOL/2Z oracle price.
    #[arg(long, value_name = "2Z_SOL_PRICE")]
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

        let convert_2z_context = Convert2zContext::try_prepare(
            &wallet,
            limit_price_str,
            source_token_account_key,
            checked_lamports,
        )
        .await?;
        println!(
            "2Z token balance: {:.8}",
            convert_2z_context.balance_before as f64 * 1e-8
        );

        let mut instructions = vec![
            convert_2z_context.instruction,
            ComputeBudgetInstruction::set_compute_unit_limit(
                Convert2zContext::BUY_SOL_COMPUTE_UNIT_LIMIT,
            ),
        ];

        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_sig = wallet.send_or_simulate_transaction(&transaction).await?;

        if let Some(tx_sig) = tx_sig {
            println!("Converted 2Z to SOL: {tx_sig}");

            let balance_after =
                fetch_token_balance(&wallet, Some(convert_2z_context.user_token_account_key))
                    .await?;
            println!(
                "Converted {:.8} 2Z tokens to {:.9} SOL",
                (convert_2z_context.balance_before - balance_after) as f64 * 1e-8,
                (convert_2z_context.required_lamports as f64 * 1e-9)
            );

            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}

//

fn parse_limit_price_to_u64(bid_price_str: String) -> Result<u64> {
    const RATE_PRECISION: f64 = doublezero_sol_conversion_interface::oracle::RATE_PRECISION as f64;

    let bid_price_str = bid_price_str.trim();
    ensure!(!bid_price_str.is_empty(), "Bid price cannot be empty");

    let bid_price = bid_price_str
        .parse::<f64>()
        .map_err(|_| anyhow::anyhow!("Invalid bid price: '{bid_price_str}'"))?;
    ensure!(bid_price > 0.0, "Bid price must be a positive value");
    ensure!(
        bid_price <= (u64::MAX as f64 / RATE_PRECISION),
        "Bid price too large"
    );

    // Check that value is at most 8 decimal places.
    if let Some(decimal_index) = bid_price_str.find('.') {
        let decimal_places = bid_price_str.len() - decimal_index - 1;
        ensure!(
            decimal_places <= 8,
            "Bid price cannot have more than 8 decimal places"
        );
    }

    Ok((bid_price * RATE_PRECISION).round() as u64)
}

pub fn unwrap_token_account_or_ata(
    wallet: &Wallet,
    source_token_account_key: Option<Pubkey>,
) -> Pubkey {
    source_token_account_key.unwrap_or(
        spl_associated_token_account_interface::address::get_associated_token_address(
            &wallet.pubkey(),
            &DOUBLEZERO_MINT_KEY,
        ),
    )
}

pub async fn fetch_token_balance(
    wallet: &Wallet,
    source_token_account_key: Option<Pubkey>,
) -> Result<u64> {
    let user_token_account_key = unwrap_token_account_or_ata(wallet, source_token_account_key);

    let token_account = wallet
        .connection
        .get_account(&user_token_account_key)
        .await
        .with_context(|| format!("Token account not found: {user_token_account_key}"))?;

    spl_token::state::Account::unpack(&token_account.data)
        .map(|account| account.amount)
        .with_context(|| format!("Account {user_token_account_key} not token account"))
}

pub struct Convert2zContext {
    pub instruction: Instruction,
    pub user_token_account_key: Pubkey,
    pub balance_before: u64,
    pub required_lamports: u64,
}

impl Convert2zContext {
    pub const BUY_SOL_COMPUTE_UNIT_LIMIT: u32 = 80_000;

    pub async fn try_prepare(
        wallet: &Wallet,
        limit_price_str: Option<String>,
        source_token_account_key: Option<Pubkey>,
        checked_lamports: Option<u64>,
    ) -> Result<Self> {
        let wallet_key = wallet.pubkey();

        let SolConversionState {
            program_state: (_, sol_conversion_program_state),
            configuration_registry: (_, configuration_registry),
            journal: (_, journal),
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

        let user_token_account_key = unwrap_token_account_or_ata(wallet, source_token_account_key);
        let balance_before = fetch_token_balance(wallet, Some(user_token_account_key)).await?;

        let oracle_price_data = try_request_oracle_conversion_price().await?;

        let limit_price = match limit_price_str {
            Some(limit_price_str) => parse_limit_price_to_u64(limit_price_str)?,
            None => oracle_price_data.swap_rate,
        };

        let instruction = try_build_instruction(
            &ID,
            BuySolAccounts::new(
                &sol_conversion_program_state.fills_registry_key,
                &user_token_account_key,
                &DOUBLEZERO_MINT_KEY,
                &wallet_key,
            ),
            &SolConversionInstructionData::BuySol {
                limit_price,
                oracle_price_data,
            },
        )
        .context("Failed to build buy SOL instruction")?;

        Ok(Self {
            instruction,
            user_token_account_key,
            balance_before,
            required_lamports,
        })
    }
}
