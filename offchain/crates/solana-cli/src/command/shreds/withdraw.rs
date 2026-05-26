use std::net::Ipv4Addr;

use anyhow::{Context, Result, bail};
use clap::Args;
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, TransactionOutcome, Wallet};
use doublezero_solana_sdk::{
    environment_usdc_token_mint_key,
    shred_subscription::{
        ID,
        instruction::{
            ShredSubscriptionInstructionData,
            account::{
                ClosePaymentEscrowAccounts, RequestInstantSeatWithdrawalAccounts,
                RequestProratedInstantSeatWithdrawalAccounts,
            },
        },
        state,
    },
    try_build_instruction,
};
use solana_sdk::{compute_budget::ComputeBudgetInstruction, pubkey::Pubkey};

/*
   doublezero-solana shreds withdraw \
       --device <PUBKEY> | --device-code <CODE> \
       --client-ip <IP> [--funds-only] [--usdc-mint <PUBKEY>] [--refund-token-account <PUBKEY>]
*/

#[derive(Debug, Args)]
pub struct WithdrawCommand {
    #[command(flatten)]
    device_args: super::DeviceArgs,
    /// Client IPv4 address
    #[arg(long)]
    client_ip: Ipv4Addr,
    /// Only close the payment escrow and withdraw USDC funds, without
    /// requesting an instant seat withdrawal.
    #[arg(long)]
    funds_only: bool,
    /// USDC mint (auto-detected from network: mainnet or development)
    #[arg(long, hide = true)]
    usdc_mint: Option<Pubkey>,
    /// USDC token account to receive the refund (defaults to your ATA)
    #[arg(long)]
    refund_token_account: Option<Pubkey>,
    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,
}

impl WithdrawCommand {
    pub async fn try_into_execute(self, dz_ledger_url: Option<String>) -> Result<()> {
        let moniker_env = self.solana_payer_options.connection_options.moniker_env();
        let dz_connection = self
            .solana_payer_options
            .connection_options
            .clone()
            .into_shred_subscription_connection();
        let wallet = Wallet::try_new(self.solana_payer_options, Some(dz_connection))?;
        let wallet_key = wallet.pubkey();

        println!("Shred subscription - Withdraw (Close Payment Escrow)");

        let network_env = match moniker_env {
            Some(env) => env,
            None => wallet.connection.try_network_environment().await?,
        };
        println!("Connected to Solana: {network_env:?}");

        let device = self
            .device_args
            .resolve(network_env, &dz_ledger_url)
            .await?;

        let usdc_mint_key = self
            .usdc_mint
            .unwrap_or(environment_usdc_token_mint_key(network_env));

        let client_ip_bits = u32::from(self.client_ip);
        let (client_seat_key, _) = state::find_client_seat_address(&device, client_ip_bits);
        let (escrow_key, _) = state::find_payment_escrow_address(&client_seat_key, &wallet_key);
        let (program_config_key, _) = state::find_program_config_address();
        let (allocation_request_key, _) =
            state::find_instant_allocation_request_address(&device, client_ip_bits);

        // Fetch client seat, payment escrow, program config, and any in-flight
        // instant seat allocation request in a single RPC.
        let mut accounts = wallet
            .connection
            .get_multiple_accounts(&[
                client_seat_key,
                escrow_key,
                program_config_key,
                allocation_request_key,
            ])
            .await?;

        // Pop in reverse order: allocation_request (3), program_config (2), escrow (1), seat (0).
        let allocation_request_in_flight = accounts.pop().flatten().is_some();
        let prorated_service_enabled = accounts
            .pop()
            .flatten()
            .is_some_and(|a| state::is_prorated_service_enabled(&a.data));
        let escrow_exists = accounts.pop().flatten().is_some();

        if allocation_request_in_flight {
            bail!(
                "Instant seat allocation request {allocation_request_key} is in flight for \
                 client seat {client_seat_key}. Wait for the oracle to ack or reject it before \
                 withdrawing."
            );
        }

        let seat_data = accounts
            .pop()
            .flatten()
            .with_context(|| format!("Client seat {client_seat_key} does not exist"))?;
        let (_, _, _, _, active_epoch) = state::parse_client_seat(&seat_data.data)
            .with_context(|| format!("Failed to parse client seat {client_seat_key}"))?;
        // Pre-upgrade seats carry `last_usdc_price_dollars == 0` and the
        // prorated instruction reverts for them. Fall back to the legacy
        // instruction until the next settlement cycle repopulates the field.
        let seat_has_recorded_price =
            state::parse_client_seat_last_usdc_price_dollars(&seat_data.data)
                .is_some_and(|price| price > 0);
        let current_epoch = wallet.connection.get_epoch_info().await?.epoch;
        let has_active_service = active_epoch >= current_epoch;

        // Only request instant withdrawal when the seat is active. Stale seats
        // (or --funds-only) skip the request and just close the payment escrow.
        let request_instant_withdrawal = has_active_service && !self.funds_only;

        if !request_instant_withdrawal && !escrow_exists {
            bail!(
                "Client seat {client_seat_key} has no payment escrow to close and no active service to withdraw"
            );
        }

        let mut instructions = vec![super::build_check_cli_version_instruction()?];
        let mut compute_unit_limit = 5_000 + 30_000;

        if request_instant_withdrawal {
            if prorated_service_enabled && seat_has_recorded_price {
                instructions.push(try_build_instruction(
                    &ID,
                    RequestProratedInstantSeatWithdrawalAccounts::new(
                        &device,
                        client_ip_bits,
                        &wallet_key,
                        active_epoch,
                        &usdc_mint_key,
                        &wallet_key,
                    ),
                    &ShredSubscriptionInstructionData::RequestProratedInstantSeatWithdrawal,
                )?);
            } else {
                instructions.push(try_build_instruction(
                    &ID,
                    RequestInstantSeatWithdrawalAccounts::new(&device, client_ip_bits, &wallet_key),
                    &ShredSubscriptionInstructionData::RequestInstantSeatWithdrawal,
                )?);
            }
            compute_unit_limit += 50_000;
        }

        if escrow_exists {
            instructions.push(try_build_instruction(
                &ID,
                ClosePaymentEscrowAccounts::new(
                    &device,
                    client_ip_bits,
                    &wallet_key,
                    &usdc_mint_key,
                    self.refund_token_account.as_ref(),
                ),
                &ShredSubscriptionInstructionData::ClosePaymentEscrow,
            )?);
        }

        instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
            compute_unit_limit,
        ));

        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

        if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
            println!("Withdraw: {tx_sig}");
            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}
