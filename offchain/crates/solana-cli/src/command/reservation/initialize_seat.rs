use std::net::Ipv4Addr;

use anyhow::Result;
use clap::Args;
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, TransactionOutcome, Wallet};
use doublezero_solana_sdk::{
    reservation::{
        ID,
        instruction::{
            ReservationInstructionData,
            account::{InitializeClientSeatAccounts, InitializePaymentEscrowAccounts},
        },
        state,
    },
    try_build_instruction,
};
use solana_sdk::compute_budget::ComputeBudgetInstruction;

/*
   doublezero-solana reservation initialize-seat \
       --device <PUBKEY> | --device-code <CODE> \
       --client-ip <IP>
*/

#[derive(Debug, Args)]
pub struct InitializeSeatCommand {
    #[command(flatten)]
    device_args: super::DeviceArgs,
    /// Client IPv4 address
    #[arg(long)]
    client_ip: Ipv4Addr,

    #[command(flatten)]
    solana_payer_options: SolanaPayerOptions,
}

impl InitializeSeatCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let wallet = Wallet::try_from(self.solana_payer_options)?;
        let wallet_key = wallet.pubkey();

        println!("Reservation - Initialize Client Seat");

        let network_env = wallet.connection.try_network_environment().await?;
        println!("Connected to Solana: {network_env:?}");

        let device = self.device_args.resolve(network_env).await?;
        let client_ip_bits = u32::from(self.client_ip);

        // Derive PDAs.
        let (client_seat_key, seat_bump) = state::find_client_seat_address(&device, client_ip_bits);
        let (_, escrow_bump) = state::find_payment_escrow_address(&client_seat_key, &wallet_key);

        // Check if the client seat already exists on-chain.
        let seat_exists = wallet
            .connection
            .get_account(&client_seat_key)
            .await
            .is_ok();

        let mut instructions = Vec::new();
        let mut compute_unit_limit = 50_000 + Wallet::compute_units_for_bump_seed(escrow_bump);

        if seat_exists {
            println!("Client seat already exists, skipping initialization.");
        } else {
            let seat_ix = try_build_instruction(
                &ID,
                InitializeClientSeatAccounts::new(&wallet_key, &device, client_ip_bits),
                &ReservationInstructionData::InitializeClientSeat {
                    client_ip: client_ip_bits,
                },
            )?;
            instructions.push(seat_ix);
            compute_unit_limit += Wallet::compute_units_for_bump_seed(seat_bump);
        }

        // InitializePaymentEscrow — creates escrow PDA for this payer.
        let escrow_ix = try_build_instruction(
            &ID,
            InitializePaymentEscrowAccounts::new(&client_seat_key, &wallet_key),
            &ReservationInstructionData::InitializePaymentEscrow,
        )?;
        instructions.push(escrow_ix);
        instructions.push(ComputeBudgetInstruction::set_compute_unit_limit(
            compute_unit_limit,
        ));

        if let Some(ref compute_unit_price_ix) = wallet.compute_unit_price_ix {
            instructions.push(compute_unit_price_ix.clone());
        }

        let transaction = wallet.new_transaction(&instructions).await?;
        let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

        if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
            println!("Initialize client seat: {tx_sig}");
            wallet.print_verbose_output(&[tx_sig]).await?;
        }

        Ok(())
    }
}
