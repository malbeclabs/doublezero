pub mod list;
pub mod pay;
pub mod payments;
pub mod price;
pub mod publisher_rewards;
pub mod validator_client_rewards;
pub mod withdraw;

use std::{io::Write, time::Duration};

use anyhow::{Result, bail};
use clap::{Args, Subcommand};
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::rpc::{DoubleZeroLedgerConnection, NetworkEnvironment};
use doublezero_solana_sdk::shred_subscription::{
    ID as SHRED_SUBSCRIPTION_PROGRAM_ID,
    instruction::{ShredSubscriptionInstructionData, account::CheckCliVersionAccounts},
};
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::{
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_sdk::pubkey::Pubkey;

// Solana's nominal slot duration. Mainnet-beta moves 400ms to 350ms at the
// start of epoch 1020 (2026-08-21) and SIMD-0525 continues stepping it down to
// 200ms, so this needs one more update when that rollout completes. Testnet is
// already at 200ms. This value deliberately does not vary by cluster, because
// both users want a deterministic, reproducible number more than an accurate
// one: one prints a "~" prefixed estimate and the other computes a deadline
// slot that the CLI and the operator must agree on.
pub(in crate::command::shreds) const NOMINAL_SLOT_DURATION: Duration = Duration::from_millis(350);

#[derive(Debug, Args)]
pub struct ShredsCommand {
    /// Override the DZ Ledger RPC URL. When omitted, the URL is derived from
    /// the Solana network environment. Required for e2e / Docker environments
    /// where the DZ Ledger runs on the same validator as the shred-subscription
    /// program.
    // `pub` so the binary can fill this slot from the global --dz-ledger-url
    // when it is not given here (the subcommand-level flag wins).
    #[arg(long, env)]
    pub dz_ledger_url: Option<String>,

    #[command(subcommand)]
    pub command: ShredsSubcommand,
}

impl ShredsCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        self.command.execute(self.dz_ledger_url, ctx, out).await
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
    /// Validator client rewards: claim accumulated rewards and manage proportions.
    ValidatorClientRewards(validator_client_rewards::ValidatorClientRewardsCommand),
    /// Validator publisher rewards configuration.
    PublisherRewards(publisher_rewards::PublisherRewardsCommand),
}

impl ShredsSubcommand {
    pub async fn execute(
        self,
        dz_ledger_url: Option<String>,
        ctx: &CliContext,
        out: &mut impl Write,
    ) -> Result<()> {
        match self {
            Self::Pay(command) => command.execute(dz_ledger_url, ctx, out).await,
            Self::Withdraw(command) => command.execute(dz_ledger_url, ctx, out).await,
            Self::List(command) => command.execute(dz_ledger_url, ctx, out).await,
            Self::Payments(command) => command.execute(dz_ledger_url, ctx, out).await,
            Self::Price(command) => command.execute(dz_ledger_url, ctx, out).await,
            Self::ValidatorClientRewards(command) => command.execute(ctx, out).await,
            Self::PublisherRewards(command) => command.execute(ctx, out).await,
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
pub(in crate::command::shreds) fn make_dz_connection(
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
pub(in crate::command::shreds) fn shred_oracle_key(env: NetworkEnvironment) -> Option<Pubkey> {
    match env {
        NetworkEnvironment::MainnetBeta => Some(solana_sdk::pubkey!(
            "3b2Ze7VYUvhwQBfx5oCMCmsc2xvyZ74s2Lata5vmQeeN"
        )),
        NetworkEnvironment::Testnet => Some(solana_sdk::pubkey!(
            "BUtAWK4GaUV42YRp7jSHZhchspsshabn67HnBHnKxzsY"
        )),
        NetworkEnvironment::Devnet => None,
        NetworkEnvironment::Localnet => None,
    }
}

/// Parse the CLI's build version into (major, minor, patch).
///
/// Handles version strings like "0.5.0" or "0.5.0-rc1" by only considering
/// the first three numeric components.
fn cli_version() -> (u32, u32, u32) {
    let version_str = option_env!("BUILD_VERSION")
        .unwrap_or(env!("CARGO_PKG_VERSION"))
        .trim_start_matches('v');
    let mut parts = version_str.split('.');
    let major = parts.next().and_then(|s| s.parse().ok()).unwrap_or(0);
    let minor = parts.next().and_then(|s| s.parse().ok()).unwrap_or(0);
    // Strip any pre-release suffix (e.g. "0-rc1" -> "0").
    let patch = parts
        .next()
        .and_then(|s| s.split('-').next())
        .and_then(|s| s.parse().ok())
        .unwrap_or(0);
    (major, minor, patch)
}

/// Build a `CheckCliVersion` instruction to prepend to write transactions.
pub(in crate::command::shreds) fn build_check_cli_version_instruction()
-> Result<solana_sdk::instruction::Instruction> {
    let (major, minor, patch) = cli_version();
    let ix = doublezero_solana_sdk::try_build_instruction(
        &SHRED_SUBSCRIPTION_PROGRAM_ID,
        CheckCliVersionAccounts::new(),
        &ShredSubscriptionInstructionData::CheckCliVersion {
            major,
            minor,
            patch,
        },
    )?;
    Ok(ix)
}

pub(in crate::command::shreds) fn serviceability_program_id(
    env: NetworkEnvironment,
) -> Result<Pubkey> {
    match env {
        NetworkEnvironment::MainnetBeta => {
            Ok(doublezero_serviceability::addresses::mainnet::program_id::id())
        }
        NetworkEnvironment::Testnet => {
            Ok(doublezero_serviceability::addresses::testnet::program_id::id())
        }
        NetworkEnvironment::Devnet => {
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
