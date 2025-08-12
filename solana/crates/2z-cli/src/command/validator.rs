use anyhow::{bail, Result};
use clap::{Args, Subcommand};
use solana_sdk::pubkey::Pubkey;

use crate::payer::SolanaPayerOptions;
use crate::rpc::{DoubleZeroLedgerRpcOptions, SolanaConnectionOptions};
use crate::serviceability::ServiceKey;

#[derive(Debug, Args)]
pub struct ValidatorCliCommand {
    #[command(subcommand)]
    pub command: ValidatorSubCommand,
}

#[derive(Debug, Subcommand)]
pub enum ValidatorSubCommand {
    /// Compute Solana validator revenue from the DoubleZero Ledger network. NOTE: This is an
    /// RPC-intensive call.
    ComputeRevenue {
        #[command(flatten)]
        epoch: DoubleZeroEpoch,

        /// Path for generated rewards JSON file. If not provided, prints to stdout.
        #[arg(long = "out", short = 'o')]
        out_filename: Option<String>,

        #[command(flatten)]
        solana_connection_options: SolanaConnectionOptions,
    },

    /// Fetch computed Solana validator revenue from the DoubleZero Ledger network.
    FetchComputedRevenue {
        #[command(flatten)]
        epoch: DoubleZeroEpoch,

        /// Path for generated rewards JSON file. If not provided, prints to stdout.
        #[arg(long = "out", short = 'o')]
        out_filename: Option<String>,

        #[command(flatten)]
        dz_ledger_rpc_options: DoubleZeroLedgerRpcOptions,
    },

    /// Pay Solana validator revenue (denominated in SOL).
    PayFee {
        #[command(flatten)]
        validator_id: ValidatorId,

        #[arg(value_parser = parse_epoch_equals_revenue)]
        epoch_revenue: Option<(u64, u64)>,

        /// Contributor rewards JSON file. Required if --epoch-revenue not provided.
        #[arg(long, short = 'f')]
        rewards_from_file: Option<String>,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    RequestAccess {
        #[command(flatten)]
        validator_id: ValidatorId,

        #[command(flatten)]
        service_key: ServiceKey,

        /// Base58-encoded signature using a validator keypair. Message to sign should take the
        /// form: "request_access::{service_key}". See `solana sign-offchain-message -h`.
        #[arg(long)]
        ed25519_signature: String,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },
}

#[derive(Debug, Args)]
pub struct DoubleZeroEpoch {
    /// DoubleZero epoch to compute revenue for.
    pub epoch: u64,
}

#[derive(Debug, Args)]
pub struct ValidatorId {
    /// Validator on DoubleZero network.
    pub validator_id: Pubkey,
}

//

fn parse_epoch_equals_revenue(s: &str) -> Result<(u64, u64)> {
    let parts = s.split('=').collect::<Vec<_>>();

    if parts.len() != 2 {
        bail!("Epoch share must be in format: EPOCH=REVENUE");
    }

    let epoch = parts[0].parse::<u64>()?;
    let revenue = parts[1].parse::<u64>()?;

    Ok((epoch, revenue))
}
