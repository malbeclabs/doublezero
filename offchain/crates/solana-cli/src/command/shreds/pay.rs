use std::net::Ipv4Addr;

use anyhow::{Result, bail};
use clap::Args;
use doublezero_serviceability::{pda::get_user_pda, state::user::UserType};
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, TransactionOutcome, Wallet};
use doublezero_solana_sdk::{
    reservation::{
        ID,
        instruction::{
            ReservationInstructionData,
            account::{
                FundPaymentEscrowUsdcAccounts, InitializeClientSeatAccounts,
                InitializePaymentEscrowAccounts, RequestInstantSeatAllocationAccounts,
            },
        },
        state,
    },
    try_build_instruction,
};
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{compute_budget::ComputeBudgetInstruction, pubkey::Pubkey};
use spl_associated_token_account_interface::address::get_associated_token_address;

use super::{make_dz_connection, serviceability_program_id};

/*
   doublezero-solana reservation pay \
       --device <PUBKEY> | --device-code <CODE> \
       --client-ip <IP> --amount <USDC_DECIMAL>
*/

#[derive(Debug, Args)]
pub struct PayCommand {
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

impl PayCommand {
    pub async fn try_into_execute(self, dz_ledger_url: Option<String>) -> Result<()> {
        let wallet = Wallet::try_from(self.solana_payer_options)?;
        let wallet_key = wallet.pubkey();

        println!("Reservation - Pay");

        let network_env = wallet.connection.try_network_environment().await?;
        println!("Connected to Solana: {network_env:?}");

        let device = self
            .device_args
            .resolve(network_env, &dz_ledger_url)
            .await?;
        let client_ip_bits = u32::from(self.client_ip);

        // Best-effort check: verify this client IP doesn't already have a Multicast
        // user on serviceability. If so, the shred oracle will fail to create a new
        // subscribe user at settlement time. This is not enforced on-chain.
        if let Ok(svc_program_id) = serviceability_program_id(network_env) {
            let dz_connection = make_dz_connection(&dz_ledger_url, network_env);
            let (user_pda, _) = get_user_pda(&svc_program_id, &self.client_ip, UserType::Multicast);
            if let Ok(Some(_)) = dz_connection
                .get_account_with_commitment(&user_pda, CommitmentConfig::confirmed())
                .await
                .map(|r| r.value)
            {
                bail!(
                    "Client IP {} already has an active multicast user on serviceability. \
                     Disconnect first (doublezero disconnect) before purchasing a \
                     shred subscription.",
                    self.client_ip,
                );
            }
        }

        // Derive PDAs.
        let (client_seat_key, seat_bump) = state::find_client_seat_address(&device, client_ip_bits);
        let (escrow_key, escrow_bump) =
            state::find_payment_escrow_address(&client_seat_key, &wallet_key);

        // Check which accounts already exist on-chain.
        let accounts = wallet
            .connection
            .get_multiple_accounts(&[client_seat_key, escrow_key])
            .await
            .unwrap();
        let seat_exists = accounts[0].is_some();
        let escrow_exists = accounts[1].is_some();

        let usdc_mint_key = self.usdc_mint.unwrap_or(*state::USDC_MINT_KEY);

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

        let mut instructions = Vec::new();
        let mut compute_unit_limit = 0u32;

        if !seat_exists {
            let seat_ix = try_build_instruction(
                &ID,
                InitializeClientSeatAccounts::new(&wallet_key, &device, client_ip_bits),
                &ReservationInstructionData::InitializeClientSeat {
                    client_ip: client_ip_bits,
                },
            )?;
            instructions.push(seat_ix);
            compute_unit_limit += 50_000 + Wallet::compute_units_for_bump_seed(seat_bump);
        }

        if !escrow_exists {
            let escrow_ix = try_build_instruction(
                &ID,
                InitializePaymentEscrowAccounts::new(&client_seat_key, &wallet_key),
                &ReservationInstructionData::InitializePaymentEscrow,
            )?;
            instructions.push(escrow_ix);
            compute_unit_limit += 50_000 + Wallet::compute_units_for_bump_seed(escrow_bump);
        }

        let source_usdc_token_account = self
            .source_token_account
            .unwrap_or_else(|| get_associated_token_address(&wallet_key, &usdc_mint_key));

        let fund_ix = try_build_instruction(
            &ID,
            FundPaymentEscrowUsdcAccounts::new(
                &exchange_key,
                &device,
                client_ip_bits,
                &wallet_key,
                &usdc_mint_key,
                &source_usdc_token_account,
                &wallet_key,
            ),
            &ReservationInstructionData::FundPaymentEscrowUsdc(amount_micro),
        )?;
        instructions.push(fund_ix);
        compute_unit_limit += 50_000;

        let request_ix = try_build_instruction(
            &ID,
            RequestInstantSeatAllocationAccounts::new(
                &exchange_key,
                &device,
                client_ip_bits,
                &wallet_key,
                &wallet_key,
            ),
            &ReservationInstructionData::RequestInstantSeatAllocation,
        )?;
        instructions.push(request_ix);
        compute_unit_limit += 50_000;

        instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
            compute_unit_limit,
        ));

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
