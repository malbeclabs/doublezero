use clap::{Args, Subcommand};
use solana_sdk::pubkey::Pubkey;

use crate::{payer::SolanaPayerOptions, rpc::SolanaConnectionOptions};

pub const DECIMAL_UNITS_PER_2Z: u64 = u64::pow(10, 8);

#[derive(Debug, Args)]
pub struct AtaCliCommand {
    #[command(subcommand)]
    pub command: AtaSubCommand,
}

#[derive(Debug, Subcommand)]
pub enum AtaSubCommand {
    Create {
        /// User pubkey, which will be airdropped gas tokens on the DoubleZero Ledger network.
        recipient: Pubkey,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    Fetch {
        /// User pubkey, which will be airdropped gas tokens on the DoubleZero Ledger network.
        recipient: Pubkey,

        #[command(flatten)]
        solana_connection_options: SolanaConnectionOptions,
    },
}

/// Accepts plain or decimal strings ("50", "0.03", ".5", "1.").
/// Any decimal places beyond 9 are truncated.
pub fn decimals_of_2z(value_str: Option<&str>) -> Option<u64> {
    value_str.and_then(|value| {
        if value == "." {
            None
        } else {
            let (whole_units, decimal_units) = value.split_once('.').unwrap_or((value, ""));

            let whole_units = if whole_units.is_empty() {
                0
            } else {
                whole_units.parse::<u64>().ok()?
            };

            let decimal_units = if decimal_units.is_empty() {
                0
            } else {
                format!("{decimal_units:0<9}")[..9].parse().ok()?
            };
            Some(
                DECIMAL_UNITS_PER_2Z
                    .saturating_mul(whole_units)
                    .saturating_add(decimal_units),
            )
        }
    })
}
