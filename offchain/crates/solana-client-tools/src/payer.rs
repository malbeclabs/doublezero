use std::path::PathBuf;

use anyhow::{Context, Result, anyhow, bail, ensure};
use clap::Args;
use solana_client::rpc_config::RpcTransactionConfig;
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{
    compute_budget::ComputeBudgetInstruction,
    instruction::Instruction,
    pubkey::Pubkey,
    signature::{Keypair, Signature},
    signer::Signer,
    transaction::{TransactionError, VersionedTransaction},
};
use solana_transaction_status_client_types::UiTransactionEncoding;

use crate::{
    log_info,
    rpc::{SolanaConnection, SolanaConnectionOptions},
    transaction::new_transaction,
};

#[derive(Debug, Args, Clone)]
pub struct SolanaPayerOptions {
    #[command(flatten)]
    pub connection_options: SolanaConnectionOptions,

    #[command(flatten)]
    pub signer_options: SolanaSignerOptions,
}

#[derive(Debug, Args, Clone)]
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
    pub connection: SolanaConnection,
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

    pub async fn new_transaction_with_additional_signers(
        &self,
        instructions: &[Instruction],
        additional_signers: &[&Keypair],
    ) -> Result<VersionedTransaction> {
        let recent_blockhash = self.connection.get_latest_blockhash().await?;

        let mut signers = Vec::with_capacity(2 + additional_signers.len());

        match self.fee_payer {
            Some(ref fee_payer) => {
                signers.push(fee_payer);

                if self.signer.pubkey() != fee_payer.pubkey() {
                    signers.push(&self.signer);
                }
            }
            None => {
                signers.push(&self.signer);
            }
        }

        signers.extend_from_slice(additional_signers);

        new_transaction(instructions, &signers, recent_blockhash)
    }

    pub async fn new_transaction(
        &self,
        instructions: &[Instruction],
    ) -> Result<VersionedTransaction> {
        self.new_transaction_with_additional_signers(instructions, &[])
            .await
    }

    pub async fn print_verbose_output(&self, tx_sigs: &[Signature]) -> Result<()> {
        if self.verbose {
            log_info!("");
            log_info!("Url: {}", self.connection.url());
            log_info!("Signer: {}", self.signer.pubkey());
            if let Some(fee_payer) = &self.fee_payer {
                log_info!("Fee payer: {}", fee_payer.pubkey());
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

        log_info!("\nTransaction details for {tx_sig}");
        log_info!("  Fee (lamports): {}", tx_meta.fee);
        log_info!(
            "  Compute units: {}",
            tx_meta.compute_units_consumed.unwrap()
        );
        log_info!("  Cost units: {}", tx_meta.cost_units.unwrap());

        log_info!("\n  Program logs:");
        tx_meta.log_messages.unwrap().iter().for_each(|log| {
            log_info!("    {log}");
        });

        Ok(())
    }

    pub async fn send_or_simulate_transaction(
        &self,
        transaction: &VersionedTransaction,
    ) -> Result<Option<Signature>> {
        if self.dry_run {
            let simulation_response = self.connection.simulate_transaction(transaction).await?;

            if let Some(tx_err) = simulation_response.value.err {
                ensure!(
                    matches!(tx_err, TransactionError::InstructionError(_, _)),
                    "Simulation failed: {tx_err}"
                );
            }

            log_info!("Simulated program logs:");
            simulation_response
                .value
                .logs
                .unwrap()
                .iter()
                .for_each(|log| {
                    log_info!("  {log}");
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
            connection: SolanaConnection::try_from(connection_options)?,
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
pub fn try_load_keypair(path: Option<PathBuf>) -> Result<Keypair> {
    let home_path = std::env::var_os("HOME").unwrap();
    let default_keypair_path = ".config/solana/id.json";

    let keypair_path = path.unwrap_or_else(|| PathBuf::from(home_path).join(default_keypair_path));
    try_load_specified_keypair(&keypair_path)
}

fn try_load_specified_keypair(path: &PathBuf) -> Result<Keypair> {
    let keypair_file = std::fs::read_to_string(path)
        .context(format!("Keypair not found at {}", path.display()))?;
    let keypair_bytes = serde_json::from_str::<Vec<u8>>(&keypair_file)
        .context(format!("Keypair not valid JSON at {}", path.display()))?;
    let default_keypair = Keypair::try_from(keypair_bytes.as_slice())
        .context(format!("Invalid keypair found at {}", path.display()))?;

    Ok(default_keypair)
}
