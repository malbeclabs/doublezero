use anyhow::Result;
use clap::{Args, Subcommand};
use doublezero_solana_client_tools::{payer::SolanaPayerOptions, rpc::SolanaConnectionOptions};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, Args)]
pub struct AtaCommand {
    #[command(subcommand)]
    pub command: AtaSubcommand,
}

#[derive(Debug, Subcommand)]
pub enum AtaSubcommand {
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

impl AtaSubcommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            AtaSubcommand::Create {
                recipient: _,
                solana_payer_options: _,
            } => {
                todo!()
            }
            AtaSubcommand::Fetch {
                recipient: _,
                solana_connection_options: _,
            } => {
                todo!()
            }
        }
    }
}
