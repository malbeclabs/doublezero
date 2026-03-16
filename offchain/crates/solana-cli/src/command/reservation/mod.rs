pub mod list;
pub mod pay;
pub mod price;
pub mod withdraw;

use anyhow::{Result, bail};
use clap::{Args, Subcommand};
use doublezero_solana_client_tools::rpc::{DoubleZeroLedgerConnection, NetworkEnvironment};
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::{
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, Args)]
pub struct ReservationCommand {
    #[command(subcommand)]
    pub command: ReservationSubcommand,
}

#[derive(Debug, Subcommand)]
pub enum ReservationSubcommand {
    /// Initialize a client seat (if needed) and fund a payment escrow with USDC.
    Pay(pay::PayCommand),
    /// Close a payment escrow and withdraw any remaining USDC.
    Withdraw(withdraw::WithdrawCommand),
    /// List client seats.
    List(list::ListCommand),
    /// Show current device pricing.
    Price(price::PriceCommand),
}

impl ReservationSubcommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            Self::Pay(command) => command.try_into_execute().await,
            Self::Withdraw(command) => command.try_into_execute().await,
            Self::List(command) => command.try_into_execute().await,
            Self::Price(command) => command.try_into_execute().await,
        }
    }
}

/// Shared device identification args. Accepts either `--device <PUBKEY>` or
/// `--device-code <CODE>` (mutually exclusive). When using `--device-code`,
/// the DZ Ledger URL and serviceability program ID are derived automatically
/// from the Solana network environment.
#[derive(Debug, Args, Clone)]
pub struct DeviceArgs {
    /// Device public key.
    #[arg(long, group = "device_id")]
    pub device: Option<Pubkey>,
    /// Human-readable device code (e.g. "MIA-1").
    #[arg(long, group = "device_id")]
    pub device_code: Option<String>,
}

impl DeviceArgs {
    /// Resolve the device pubkey. When `--device-code` is used, queries the
    /// DZ Ledger's serviceability program based on the given network environment.
    pub async fn resolve(&self, network_env: NetworkEnvironment) -> Result<Pubkey> {
        if let Some(device) = self.device {
            return Ok(device);
        }
        if let Some(ref code) = self.device_code {
            let dz_connection = DoubleZeroLedgerConnection::from(network_env);
            let program_id = serviceability_program_id(network_env)?;
            resolve_device_code(&dz_connection, &program_id, code).await
        } else {
            bail!("Either --device or --device-code must be specified");
        }
    }
}

fn serviceability_program_id(env: NetworkEnvironment) -> Result<Pubkey> {
    match env {
        NetworkEnvironment::MainnetBeta => {
            Ok(doublezero_serviceability::addresses::mainnet::program_id::id())
        }
        NetworkEnvironment::Testnet => {
            Ok(doublezero_serviceability::addresses::testnet::program_id::id())
        }
        NetworkEnvironment::Localnet => {
            bail!("Device code resolution is not supported on localnet; use --device instead")
        }
    }
}

/// Resolve a human-readable device code to a pubkey by querying the
/// serviceability program's Device accounts on the DZ Ledger.
///
/// The Device account layout (Borsh-serialized) has:
///   offset 0:   account_type (1 byte, Device = 5)
///   offset 120: code (Borsh String: 4-byte LE length + utf8 bytes)
async fn resolve_device_code(
    connection: &DoubleZeroLedgerConnection,
    program_id: &Pubkey,
    code: &str,
) -> Result<Pubkey> {
    let match_bytes = borsh::to_vec(code).expect("borsh string serialization");

    let config = RpcProgramAccountsConfig {
        filters: Some(vec![
            // AccountType::Device = 5
            RpcFilterType::Memcmp(Memcmp::new_raw_bytes(0, vec![5])),
            // code field at offset 120
            RpcFilterType::Memcmp(Memcmp::new_raw_bytes(120, match_bytes)),
        ]),
        account_config: RpcAccountInfoConfig {
            encoding: Some(UiAccountEncoding::Base64),
            ..Default::default()
        },
        ..Default::default()
    };

    let accounts = connection
        .get_program_accounts_with_config(program_id, config)
        .await?;

    match accounts.len() {
        0 => bail!("No device found with code \"{code}\""),
        1 => Ok(accounts[0].0),
        n => bail!("Ambiguous: {n} devices found with code \"{code}\""),
    }
}
