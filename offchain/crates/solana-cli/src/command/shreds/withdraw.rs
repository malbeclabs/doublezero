use std::net::Ipv4Addr;

use anyhow::{Context, Result, bail};
use clap::Args;
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, TransactionOutcome, Wallet};
use doublezero_solana_sdk::{
    shred_subscription::{
        ID,
        instruction::{
            ShredSubscriptionInstructionData,
            account::{ClosePaymentEscrowAccounts, RequestInstantSeatWithdrawalAccounts},
        },
        state,
    },
    try_build_instruction,
};
use solana_sdk::{compute_budget::ComputeBudgetInstruction, pubkey::Pubkey};

/*
   doublezero-solana shreds withdraw \
       --device <PUBKEY> | --device-code <CODE> \
       --client-ip <IP> [--usdc-mint <PUBKEY>] [--refund-token-account <PUBKEY>]
*/

#[derive(Debug, Args)]
pub struct WithdrawCommand {
    #[command(flatten)]
    device_args: super::DeviceArgs,
    /// Client IPv4 address
    #[arg(long)]
    client_ip: Ipv4Addr,
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
        let mut wallet = Wallet::try_from(self.solana_payer_options)?;
        wallet.connection = dz_connection;
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
        let usdc_mint_key = self.usdc_mint.unwrap_or_else(|| {
            if network_env.is_mainnet_beta() {
                state::MAINNET_USDC_MINT_KEY
            } else {
                state::DEVELOPMENT_USDC_MINT_KEY
            }
        });
        let client_ip_bits = u32::from(self.client_ip);
        let (client_seat_key, _) = state::find_client_seat_address(&device, client_ip_bits);
        let (escrow_key, _) = state::find_payment_escrow_address(&client_seat_key, &wallet_key);

        // Fetch client seat and payment escrow.
        let mut accounts = wallet
            .connection
            .get_multiple_accounts(&[client_seat_key, escrow_key])
            .await?;

        // Pop in reverse order: escrow (index 1) first, then seat (index 0).
        let escrow_exists = accounts.pop().flatten().is_some();

        // The seat must exist and be active in the current epoch to withdraw.
        let seat_data = accounts
            .pop()
            .flatten()
            .with_context(|| format!("Client seat {client_seat_key} does not exist"))?;
        let (_, _, _, _, active_epoch) = state::parse_client_seat(&seat_data.data)
            .with_context(|| format!("Failed to parse client seat {client_seat_key}"))?;
        let current_epoch = wallet.connection.get_epoch_info().await?.epoch;
        if active_epoch < current_epoch {
            bail!("Client seat {client_seat_key} does not have active service");
        }

        let mut instructions = Vec::new();
        let mut compute_unit_limit = 30_000;

        instructions.push(try_build_instruction(
            &ID,
            RequestInstantSeatWithdrawalAccounts::new(&device, client_ip_bits, &wallet_key),
            &ShredSubscriptionInstructionData::RequestInstantSeatWithdrawal,
        )?);
        compute_unit_limit += 50_000;

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
