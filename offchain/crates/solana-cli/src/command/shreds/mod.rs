pub mod list;
pub mod pay;
pub mod payments;
pub mod price;
pub mod validator_client_rewards;
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
pub struct ShredsCommand {
    /// Override the DZ Ledger RPC URL. When omitted, the URL is derived from
    /// the Solana network environment. Required for e2e / Docker environments
    /// where the DZ Ledger runs on the same validator as the shred-subscription
    /// program.
    #[arg(long, env)]
    dz_ledger_url: Option<String>,

    #[command(subcommand)]
    pub command: ShredsSubcommand,
}

impl ShredsCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        self.command.try_into_execute(self.dz_ledger_url).await
    }
}

#[derive(Debug, Subcommand)]
pub enum ShredsSubcommand {
    /// Initialize a client seat (if needed) and fund a payment escrow with USDC.
    Pay(pay::PayCommand),
    /// Close a payment escrow and withdraw any remaining USDC.
    Withdraw(withdraw::WithdrawCommand),
    /// List client seats.
    List(list::ListCommand),
    /// Show payment history for a client seat escrow.
    Payments(payments::PaymentsCommand),
    /// Show current device pricing.
    Price(price::PriceCommand),
    /// Set the rewards proportion for a validator client.
    #[command(hide = true)]
    ValidatorClientRewards(validator_client_rewards::ValidatorClientRewardsCommand),
}

impl ShredsSubcommand {
    pub async fn try_into_execute(self, dz_ledger_url: Option<String>) -> Result<()> {
        match self {
            Self::Pay(command) => command.try_into_execute(dz_ledger_url).await,
            Self::Withdraw(command) => command.try_into_execute(dz_ledger_url).await,
            Self::List(command) => command.try_into_execute(dz_ledger_url).await,
            Self::Payments(command) => command.try_into_execute(dz_ledger_url).await,
            Self::Price(command) => command.try_into_execute(dz_ledger_url).await,
            Self::ValidatorClientRewards(command) => command.try_into_execute().await,
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
    #[arg(long, group = "device_id", env)]
    pub device: Option<Pubkey>,
    /// Human-readable device code (e.g. "MIA-1").
    #[arg(long, group = "device_id", env)]
    pub device_code: Option<String>,
}

impl DeviceArgs {
    /// Resolve the device pubkey. When `--device-code` is used, queries the
    /// DZ Ledger's serviceability program based on the given network environment.
    pub async fn resolve(
        &self,
        network_env: NetworkEnvironment,
        dz_ledger_url: &Option<String>,
    ) -> Result<Pubkey> {
        if let Some(device) = self.device {
            return Ok(device);
        }
        if let Some(ref code) = self.device_code {
            let dz_connection = make_dz_connection(dz_ledger_url, network_env);
            let program_id = serviceability_program_id(network_env)?;
            resolve_device_code(&dz_connection, &program_id, code).await
        } else {
            bail!("Either --device or --device-code must be specified");
        }
    }
}

/// Construct a DZ Ledger connection, using the explicit URL if provided or
/// falling back to the environment-derived URL.
pub(super) fn make_dz_connection(
    dz_ledger_url: &Option<String>,
    network_env: NetworkEnvironment,
) -> DoubleZeroLedgerConnection {
    match dz_ledger_url {
        Some(url) => DoubleZeroLedgerConnection::new(url.clone()),
        None => DoubleZeroLedgerConnection::from(network_env),
    }
}

/// Known shred oracle pubkey per environment. Returns `None` on localnet
/// (the multicast-user guard is already skipped there because
/// `serviceability_program_id` returns `Err`).
pub(super) fn shred_oracle_key(env: NetworkEnvironment) -> Option<Pubkey> {
    match env {
        NetworkEnvironment::MainnetBeta => Some(solana_sdk::pubkey!(
            "3b2Ze7VYUvhwQBfx5oCMCmsc2xvyZ74s2Lata5vmQeeN"
        )),
        NetworkEnvironment::Testnet => Some(solana_sdk::pubkey!(
            "BUtAWK4GaUV42YRp7jSHZhchspsshabn67HnBHnKxzsY"
        )),
        NetworkEnvironment::Localnet => None,
    }
}

pub(super) fn serviceability_program_id(env: NetworkEnvironment) -> Result<Pubkey> {
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
