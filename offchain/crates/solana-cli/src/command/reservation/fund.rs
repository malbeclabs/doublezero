use std::net::Ipv4Addr;

use anyhow::{Result, bail};
use clap::Args;
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, TransactionOutcome, Wallet};
use doublezero_solana_sdk::{
    reservation::{
        ID,
        instruction::{ReservationInstructionData, account::FundPaymentEscrowUsdcAccounts},
        state,
    },
    try_build_instruction,
};
use solana_sdk::{compute_budget::ComputeBudgetInstruction, pubkey::Pubkey};
use spl_associated_token_account_interface::address::get_associated_token_address;

/*
   doublezero-solana reservation fund \
       --device <PUBKEY> | --device-code <CODE> \
       --client-ip <IP> --amount <USDC_DECIMAL>
*/

#[derive(Debug, Args)]
pub struct FundCommand {
    #[command(flatten)]
    device_args: super::DeviceArgs,
    /// Client IPv4 address
    #[arg(long)]
    client_ip: Ipv4Addr,
    /// Amount of USDC to fund (in decimal, e.g. 1.5 = 1_500_000 micro-USDC)
    #[arg(long)]
    amount: f64,
    /// USDC mint (defaults to mainnet USDC)
    #[arg(long, hide = true)]
    usdc_mint: Option<Pubkey>,
    /// Source USDC token account (defaults to payer's ATA)
    #[arg(long)]
    source_token_account: Option<Pubkey>,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,
}

impl FundCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let wallet = Wallet::try_from(self.solana_payer_options)?;
        let wallet_key = wallet.pubkey();

        println!("Reservation - Fund Payment Escrow (USDC)");

        let network_env = wallet.connection.try_network_environment().await?;
        println!("Connected to Solana: {network_env:?}");

        let device = self.device_args.resolve(network_env).await?;
        let usdc_mint_key = self.usdc_mint.unwrap_or(*state::USDC_MINT_KEY);
        let client_ip_bits = u32::from(self.client_ip);

        // Convert decimal USDC to micro-USDC (6 decimals).
        if self.amount <= 0.0 {
            bail!("Amount must be a positive value");
        }
        let amount_micro = (self.amount * 1_000_000.0).round() as u64;

        // Derive the exchange key from the on-chain DeviceHistory account.
        let device_history_key = state::find_device_history_address(&device).0;
        let device_history_account = wallet.connection.get_account(&device_history_key).await?;
        let device_info = state::parse_device_history(&device_history_account.data)
            .ok_or_else(|| anyhow::anyhow!("Failed to parse DeviceHistory account"))?;
        let exchange_key = device_info.exchange_key;

        // Check the current price so the user gets a friendly error instead of
        // an opaque on-chain revert.
        let metro_history_key = state::find_metro_history_address(&exchange_key).0;
        let metro_history_account = wallet.connection.get_account(&metro_history_key).await?;
        if let Some(metro_info) = state::parse_metro_history(&metro_history_account.data) {
            let min_price = (metro_info.current_usdc_price as i32
                + device_info.current_premium as i32)
                .max(0) as u64;
            if amount_micro < min_price {
                let min_usdc = min_price as f64 / 1_000_000.0;
                bail!(
                    "Amount ({:.6} USDC) is below the current price ({:.6} USDC)",
                    self.amount,
                    min_usdc,
                );
            }
        }

        let source_usdc_token_account = self
            .source_token_account
            .unwrap_or_else(|| get_associated_token_address(&wallet_key, &usdc_mint_key));

        let accounts = FundPaymentEscrowUsdcAccounts::new(
            &exchange_key,
            &device,
            client_ip_bits,
            &wallet_key,
            &usdc_mint_key,
            &source_usdc_token_account,
            &wallet_key,
        );

        let ix = try_build_instruction(
            &ID,
            accounts,
            &ReservationInstructionData::FundPaymentEscrowUsdc(amount_micro),
        )?;

        let compute_unit_limit = 50_000;

        let mut instructions = vec![
            ix,
            ComputeBudgetInstruction::set_compute_unit_limit(compute_unit_limit),
        ];

        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

        if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
            println!("Fund escrow ({} USDC): {tx_sig}", self.amount);
            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}
