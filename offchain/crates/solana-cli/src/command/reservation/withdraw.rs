use std::net::Ipv4Addr;

use anyhow::{Result, bail};
use clap::Args;
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, TransactionOutcome, Wallet};
use doublezero_solana_sdk::{
    reservation::{
        ID,
        instruction::{ReservationInstructionData, account::ClosePaymentEscrowAccounts},
        state,
    },
    try_build_instruction,
};
use solana_sdk::{compute_budget::ComputeBudgetInstruction, pubkey::Pubkey};

/*
   doublezero-solana reservation withdraw \
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
    /// USDC mint (defaults to mainnet USDC)
    #[arg(long, hide = true)]
    usdc_mint: Option<Pubkey>,
    /// USDC token account to receive the refund (defaults to your ATA)
    #[arg(long)]
    refund_token_account: Option<Pubkey>,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,
}

impl WithdrawCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let wallet = Wallet::try_from(self.solana_payer_options)?;
        let wallet_key = wallet.pubkey();

        println!("Reservation - Withdraw (Close Payment Escrow)");

        let network_env = wallet.connection.try_network_environment().await?;
        println!("Connected to Solana: {network_env:?}");

        let device = self.device_args.resolve(network_env).await?;
        let usdc_mint_key = self.usdc_mint.unwrap_or(*state::USDC_MINT_KEY);
        let client_ip_bits = u32::from(self.client_ip);
        let (client_seat_key, _) = state::find_client_seat_address(&device, client_ip_bits);

        // Verify the payment escrow exists before submitting the transaction.
        let (escrow_key, _) = state::find_payment_escrow_address(&client_seat_key, &wallet_key);
        if wallet.connection.get_account(&escrow_key).await.is_err() {
            bail!("No payment escrow found for this seat and wallet. Nothing to withdraw.");
        }

        let accounts = ClosePaymentEscrowAccounts::new(
            &device,
            client_ip_bits,
            &wallet_key,
            &usdc_mint_key,
            self.refund_token_account.as_ref(),
        );

        let ix = try_build_instruction(
            &ID,
            accounts,
            &ReservationInstructionData::ClosePaymentEscrow,
        )?;

        let compute_unit_limit = 30_000;

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
            println!("Withdraw: {tx_sig}");
            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}
