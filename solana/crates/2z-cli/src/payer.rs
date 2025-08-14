use std::path::PathBuf;

use anyhow::{anyhow, bail, Result};
use clap::Args;
use solana_client::rpc_config::RpcTransactionConfig;
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{
    compute_budget::ComputeBudgetInstruction,
    instruction::Instruction,
    pubkey::Pubkey,
    signature::{Keypair, Signature},
    signer::Signer,
    transaction::VersionedTransaction,
};
use solana_transaction_status_client_types::UiTransactionEncoding;

use crate::{
    rpc::{Connection, SolanaConnectionOptions},
    transaction::new_transaction,
};

#[derive(Debug, Args)]
pub struct SolanaPayerOptions {
    #[command(flatten)]
    pub connection_options: SolanaConnectionOptions,

    #[command(flatten)]
    pub signer_options: SolanaSignerOptions,
}

#[derive(Debug, Args)]
pub struct SolanaSignerOptions {
    /// Filepath or URL to a keypair.
    #[arg(long = "keypair", short = 'k', value_name = "KEYPAIR")]
    pub keypair_path: Option<String>,

    /// Set the compute unit price for transaction in increments of 0.000001 lamports per compute
    /// unit.
    #[arg(long, value_name = "MICROLAMPORTS")]
    pub with_compute_unit_price: Option<u64>,

    /// Print verbose output.
    #[arg(long, short = 'v', value_name = "VERBOSE", default_value = "false")]
    pub verbose: bool,

    /// Filepath or URL to keypair to pay transaction fee.
    #[arg(long = "fee-payer", value_name = "KEYPAIR")]
    pub fee_payer_path: Option<String>,

    /// Simulate transaction only.
    #[arg(long, value_name = "DRY_RUN")]
    pub dry_run: bool,
}

pub struct Wallet {
    pub connection: Connection,
    pub signer: Keypair,
    pub compute_unit_price_ix: Option<Instruction>,
    pub verbose: bool,
    pub fee_payer: Option<Keypair>,
    pub dry_run: bool,
}

impl Wallet {
    pub fn pubkey(&self) -> Pubkey {
        self.signer.pubkey()
    }

    pub async fn new_transaction(
        &self,
        instructions: &[Instruction],
    ) -> Result<VersionedTransaction> {
        let recent_blockhash = self.connection.get_latest_blockhash().await?;

        let transaction = if let Some(ref fee_payer) = self.fee_payer {
            new_transaction(instructions, &[fee_payer, &self.signer], recent_blockhash)
        } else {
            new_transaction(instructions, &[&self.signer], recent_blockhash)
        };

        Ok(transaction)
    }

    pub async fn print_verbose_output(&self, tx_sigs: &[Signature]) -> Result<()> {
        if self.verbose {
            println!();
            println!("Url: {}", self.connection.url());
            println!("Signer: {}", self.signer.pubkey());
            if let Some(fee_payer) = &self.fee_payer {
                println!("Fee payer: {}", fee_payer.pubkey());
            }

            for tx_sig in tx_sigs {
                self.print_transaction_details(tx_sig).await?;
            }
        }

        Ok(())
    }

    async fn print_transaction_details(&self, tx_sig: &Signature) -> Result<()> {
        let tx_response = self
            .connection
            .get_transaction_with_config(
                tx_sig,
                RpcTransactionConfig {
                    encoding: Some(UiTransactionEncoding::JsonParsed),
                    commitment: Some(CommitmentConfig::confirmed()),
                    max_supported_transaction_version: Some(0),
                },
            )
            .await?;

        let tx_meta = tx_response
            .transaction
            .meta
            .ok_or_else(|| anyhow!("Transaction meta not found"))?;

        println!("\nTransaction details for {tx_sig}");
        println!("  Fee (lamports): {}", tx_meta.fee);
        println!(
            "  Compute units: {}",
            tx_meta.compute_units_consumed.unwrap()
        );
        println!("  Cost units: {}", tx_meta.cost_units.unwrap());

        println!("\n  Program logs:");
        tx_meta.log_messages.unwrap().iter().for_each(|log| {
            println!("    {log}");
        });

        Ok(())
    }

    pub async fn send_or_simulate_transaction(
        &self,
        transaction: &VersionedTransaction,
    ) -> Result<Option<Signature>> {
        if self.dry_run {
            let simulation_response = self.connection.simulate_transaction(transaction).await?;

            println!("Simulated program logs:");
            simulation_response
                .value
                .logs
                .unwrap()
                .iter()
                .for_each(|log| {
                    println!("  {log}");
                });

            Ok(None)
        } else {
            let tx_sig = self
                .connection
                .send_and_confirm_transaction_with_spinner(transaction)
                .await?;

            Ok(Some(tx_sig))
        }
    }

    pub fn compute_units_for_bump_seed(bump: u8) -> u32 {
        1_500 * u32::from(255 - bump)
    }
}

impl TryFrom<SolanaPayerOptions> for Wallet {
    type Error = anyhow::Error;

    fn try_from(opts: SolanaPayerOptions) -> Result<Wallet> {
        let SolanaPayerOptions {
            connection_options,
            signer_options:
                SolanaSignerOptions {
                    keypair_path,
                    with_compute_unit_price,
                    verbose,
                    fee_payer_path,
                    dry_run,
                },
        } = opts;

        let signer = try_load_keypair(keypair_path.map(Into::into))?;

        let fee_payer = match fee_payer_path {
            Some(path) => {
                let payer_signer = try_load_specified_keypair(&PathBuf::from(path))?;
                if payer_signer.pubkey() == signer.pubkey() {
                    bail!("Specify fee payer if it differs from the main keypair");
                }

                Some(payer_signer)
            }
            None => None,
        };

        Ok(Wallet {
            connection: Connection::try_from(connection_options)?,
            signer,
            compute_unit_price_ix: with_compute_unit_price
                .map(ComputeBudgetInstruction::set_compute_unit_price),
            verbose,
            fee_payer,
            dry_run,
        })
    }
}

/// Taken from a Solana cookbook to load a keypair from a user's Solana config
/// location.
fn try_load_keypair(path: Option<PathBuf>) -> Result<Keypair> {
    let home_path = std::env::var_os("HOME").unwrap();
    let default_keypair_path = ".config/solana/id.json";

    let keypair_path = path.unwrap_or_else(|| PathBuf::from(home_path).join(default_keypair_path));
    try_load_specified_keypair(&keypair_path)
}

fn try_load_specified_keypair(path: &PathBuf) -> Result<Keypair> {
    let keypair_file = std::fs::read_to_string(path)?;
    let keypair_bytes = serde_json::from_str::<Vec<u8>>(&keypair_file)?;
    let default_keypair = Keypair::try_from(keypair_bytes.as_slice())?;

    Ok(default_keypair)
}
