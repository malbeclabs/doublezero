mod jupiter;

use anyhow::{Context, Result, bail, ensure};
use clap::Args;
use doublezero_solana_client_tools::{
    instruction::take_instruction,
    payer::{SolanaPayerOptions, TransactionOutcome, Wallet},
};
use doublezero_solana_sdk::revenue_distribution::env::mainnet::DOUBLEZERO_MINT_KEY;
use jupiter::{JupiterClient, quote::JupiterLegacyQuoteResponse};
use solana_client::rpc_config::{
    RpcSimulateTransactionAccountsConfig, RpcSimulateTransactionConfig,
};
use solana_sdk::{
    compute_budget::ComputeBudgetInstruction, native_token::LAMPORTS_PER_SOL, program_pack::Pack,
    pubkey::Pubkey,
};

use crate::command::revenue_distribution::{SolConversionState, convert_2z::Convert2zContext};

const DEFAULT_BUY_SOL_ADDRESS_LOOKUP_TABLE_KEY: Pubkey =
    solana_sdk::pubkey!("GnwZZZVudHSqChJiAh1RULWJe2itLHSZ9HCNXrbBQKPs");

const TOKEN_ACCOUNT_RENT_EXEMPTION_LAMPORTS: u64 = 2_039_280;

#[derive(Debug, Args, Clone)]
pub struct Harvest2zCommand {
    /// See https://dev.jup.ag/api-reference/swap/program-id-to-label for available
    /// program ID labels.
    #[arg(long, value_name = "JUPITER_LABEL")]
    specific_dex: Option<String>,

    /// Jupiter API key for authenticated access. If not provided, falls back
    /// to the legacy lite-api.jup.ag endpoint (deprecated Jan 31 2026).
    #[arg(long, value_name = "API_KEY")]
    jupiter_api_key: Option<String>,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,
}

impl Harvest2zCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let Self {
            specific_dex,
            jupiter_api_key,
            solana_payer_options,
        } = self;

        let jupiter_client = JupiterClient::new(jupiter_api_key.as_deref())?;

        let wallet = Wallet::try_from(solana_payer_options)?;
        ensure!(
            wallet.compute_unit_price_ix.is_none(),
            "Compute unit price is not supported for harvest-2z command"
        );

        let wallet_key = wallet.pubkey();
        let lamports_balance_before = wallet.connection.get_balance(&wallet_key).await?;

        let sol_conversion_state = SolConversionState::try_fetch(&wallet.connection).await?;
        let fixed_fill_quantity = sol_conversion_state.fixed_fill_quantity;

        let mut convert_2z_context = Convert2zContext::try_prepare(
            &wallet,
            &sol_conversion_state,
            None, //limit_price_str
            None, //source_token_account_key
            None, //checked_lamports
        )
        .await?;
        let buy_sol_ix = take_instruction(&mut convert_2z_context.instruction);

        ensure!(
            lamports_balance_before >= fixed_fill_quantity,
            "Not enough SOL to cover conversion. Need at least {:0.9} SOL",
            fixed_fill_quantity as f64 * 1e-9,
        );

        let mut input_sol_amount = fixed_fill_quantity - 5_000;

        let token_balance_before = match convert_2z_context
            .try_token_balance(&wallet.connection)
            .await
        {
            Ok(token_balance) => token_balance,
            Err(_) => {
                input_sol_amount -= TOKEN_ACCOUNT_RENT_EXEMPTION_LAMPORTS;
                0
            }
        };

        let mut quote_response = try_quote_sol_to_2z(
            &jupiter_client,
            input_sol_amount,
            convert_2z_context.discount_params.max_discount,
            specific_dex,
        )
        .await?;

        let discounted_swap_rate = convert_2z_context.limit_price;
        let min_amount_out = u128::from(discounted_swap_rate) * u128::from(input_sol_amount)
            / u128::from(LAMPORTS_PER_SOL);
        let min_amount_out =
            u64::try_from(min_amount_out).context("Overflow when calculating min amount out")?;
        override_quote_response(&mut quote_response, min_amount_out);

        let swap_request = jupiter::swap_instructions::JupiterLegacySwapInstructionsRequest {
            user_public_key: wallet_key.to_string(),
            quote_response,
            wrap_and_unwrap_sol: Some(true),
            ..Default::default()
        };

        let jupiter::swap_instructions::JupiterLegacySwapInstructionsResponse {
            compute_budget_instructions: _,
            setup_instructions: jupiter_setup_instructions,
            swap_instruction: jupiter_swap_instruction,
            cleanup_instruction: jupiter_cleanup_instruction,
            other_instructions: jupiter_other_instructions,
            address_lookup_table_addresses,
        } = swap_request.try_execute(&jupiter_client).await?;

        let mut instructions = Vec::new();
        for jup_ix in jupiter_setup_instructions {
            instructions.push(jup_ix.try_into()?);
        }

        instructions.push(jupiter_swap_instruction.try_into()?);

        if let Some(jup_ix) = jupiter_cleanup_instruction {
            instructions.push(jup_ix.try_into()?);
        }

        for jup_ix in jupiter_other_instructions {
            instructions.push(jup_ix.try_into()?);
        }

        instructions.push(buy_sol_ix);
        instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(420_000));

        let mut address_lookup_table_keys = address_lookup_table_addresses
            .iter()
            .map(|s| Pubkey::from_str_const(s))
            .collect::<Vec<_>>();
        address_lookup_table_keys.push(DEFAULT_BUY_SOL_ADDRESS_LOOKUP_TABLE_KEY);

        let transaction = wallet
            .new_transaction_with_additional_signers_and_lookup_tables(
                &instructions,
                &[],
                &address_lookup_table_keys,
            )
            .await?;
        let tx_outcome = wallet
            .send_or_simulate_transaction_with_configs(
                &transaction,
                wallet.default_send_transaction_config(),
                RpcSimulateTransactionConfig {
                    accounts: Some(RpcSimulateTransactionAccountsConfig {
                        encoding: Default::default(),
                        addresses: vec![
                            wallet_key.to_string(),
                            convert_2z_context.user_token_account_key.to_string(),
                        ],
                    }),
                    ..wallet.default_simulate_transaction_config()
                },
            )
            .await?;

        match tx_outcome {
            TransactionOutcome::Executed(tx_sig) => {
                println!("Harvested 2Z tokens: {tx_sig}");

                let token_balance_after = convert_2z_context
                    .try_token_balance(&wallet.connection)
                    .await?;
                println!(
                    "Harvested {:.8} 2Z tokens with {:.9} SOL",
                    (token_balance_after - token_balance_before) as f64 * 1e-8,
                    (fixed_fill_quantity as f64 * 1e-9)
                );

                wallet.print_verbose_output(&[tx_sig]).await?;
            }
            TransactionOutcome::Simulated(simulation_response) => {
                let mut post_simulation_account_infos = simulation_response
                    .accounts
                    .unwrap()
                    .into_iter()
                    .flatten()
                    .collect::<Vec<_>>();
                ensure!(
                    post_simulation_account_infos.len() == 2,
                    "Expected 2 accounts after simulation, got {}",
                    post_simulation_account_infos.len()
                );

                let ata_account_data = post_simulation_account_infos
                    .pop()
                    .unwrap()
                    .data
                    .decode()
                    .context("Failed to decode ATA account info")?;
                let token_balance_after =
                    spl_token_interface::state::Account::unpack(&ata_account_data)
                        .unwrap()
                        .amount;
                ensure!(
                    token_balance_after >= token_balance_before,
                    "Simulated harvesting 2Z tokens failed"
                );
                println!(
                    "Simulated harvesting {:.8} 2Z tokens with {:.9} SOL",
                    (token_balance_after - token_balance_before) as f64 * 1e-8,
                    (fixed_fill_quantity as f64 * 1e-9)
                );

                let lamports_balance_after = post_simulation_account_infos.pop().unwrap().lamports;
                ensure!(
                    lamports_balance_after == lamports_balance_before,
                    "SOL balance changed after simulation"
                );
            }
        }

        Ok(())
    }
}

async fn try_quote_sol_to_2z(
    jupiter_client: &JupiterClient,
    amount: u64,
    max_discount_rate: u64,
    specific_dex: Option<String>,
) -> Result<JupiterLegacyQuoteResponse> {
    let slippage_bps = u16::try_from(max_discount_rate)
        .context("Overflow when calculating slippage bps with max discount rate")?;

    let quote_request = jupiter::quote::JupiterLegacyQuoteRequest {
        slippage_bps,
        restrict_intermediate_tokens: Some(true),
        amount,
        output_mint: DOUBLEZERO_MINT_KEY.to_string(),
        input_mint: spl_token_interface::native_mint::ID.to_string(),
        dexes: specific_dex,
        ..Default::default()
    };

    for _ in 0..5 {
        let response = quote_request.try_execute(jupiter_client).await?;

        // Any route plans that involve more intermediate steps will not fit in
        // the transaction.
        if response.route_plan.len() <= 2 {
            return Ok(response);
        }

        println!("Waiting for quote response to be updated...");
        tokio::time::sleep(tokio::time::Duration::from_millis(400)).await;
    }

    bail!("Failed to get valid quote response in 5 attempts");
}

fn override_quote_response(response: &mut JupiterLegacyQuoteResponse, min_amount_out: u64) {
    let min_amount_out_str = min_amount_out.to_string();

    response.price_impact_pct = "0.0".to_string();
    response.out_amount = min_amount_out_str.clone();
    response.other_amount_threshold = min_amount_out_str.clone();

    // Last leg of the swap is XYZ -> 2Z.
    let last_leg = response.route_plan.last_mut().unwrap();
    last_leg.swap_info.out_amount = min_amount_out_str;
}
