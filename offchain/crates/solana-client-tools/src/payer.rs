use std::path::PathBuf;

use anyhow::{Context, Result, ensure};
use clap::Args;
use solana_client::{
    rpc_config::{RpcSendTransactionConfig, RpcSimulateTransactionConfig, RpcTransactionConfig},
    rpc_response::RpcSimulateTransactionResult,
};
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{
    address_lookup_table::state::AddressLookupTable,
    compute_budget::ComputeBudgetInstruction,
    instruction::Instruction,
    message::AddressLookupTableAccount,
    pubkey::Pubkey,
    signature::{Keypair, Signature},
    signer::Signer,
    transaction::{TransactionError, VersionedTransaction},
};
use solana_transaction_status_client_types::UiTransactionEncoding;

// Re-export for backward compatibility
pub use crate::keypair::try_load_keypair;
use crate::{
    rpc::{SolanaConnection, SolanaConnectionOptions},
    transaction::try_new_transaction,
};

#[derive(Debug, Args, Clone, Default)]
pub struct SolanaPayerOptions {
    #[command(flatten)]
    pub connection_options: SolanaConnectionOptions,

    #[command(flatten)]
    pub signer_options: SolanaSignerOptions,
}

#[derive(Debug, Args, Clone, Default)]
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

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum TransactionOutcome {
    Simulated(RpcSimulateTransactionResult),
    Executed(Signature),
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

    pub async fn new_transaction_with_additional_signers_and_lookup_tables(
        &self,
        instructions: &[Instruction],
        additional_signers: &[&Keypair],
        address_lookup_table_keys: &[Pubkey],
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

        if address_lookup_table_keys.is_empty() {
            return try_new_transaction(instructions, &signers, &[], recent_blockhash);
        }

        let lut_account_infos = self
            .connection
            .get_multiple_accounts(address_lookup_table_keys)
            .await
            .context("Failed to get address lookup table accounts")?
            .into_iter()
            .flatten()
            .collect::<Vec<_>>();
        ensure!(
            lut_account_infos.len() == address_lookup_table_keys.len(),
            "Expected {} address lookup table accounts, got {}",
            address_lookup_table_keys.len(),
            lut_account_infos.len()
        );

        let address_lookup_table_accounts = lut_account_infos
            .into_iter()
            .zip(address_lookup_table_keys)
            .map(|(account_info, key)| {
                let lut =
                    AddressLookupTable::deserialize(&account_info.data).with_context(|| {
                        format!("Failed to deserialize {key} as address lookup table")
                    })?;

                Ok(AddressLookupTableAccount {
                    key: *key,
                    addresses: lut.addresses.into_owned(),
                })
            })
            .collect::<Result<Vec<_>>>()?;

        try_new_transaction(
            instructions,
            &signers,
            &address_lookup_table_accounts,
            recent_blockhash,
        )
    }

    pub async fn new_transaction(
        &self,
        instructions: &[Instruction],
    ) -> Result<VersionedTransaction> {
        self.new_transaction_with_additional_signers_and_lookup_tables(instructions, &[], &[])
            .await
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
            .context("Transaction meta not found")?;

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
    ) -> Result<TransactionOutcome> {
        self.send_or_simulate_transaction_with_configs(
            transaction,
            self.default_send_transaction_config(),
            self.default_simulate_transaction_config(),
        )
        .await
    }

    pub async fn send_or_simulate_transaction_with_configs(
        &self,
        transaction: &VersionedTransaction,
        send_config: RpcSendTransactionConfig,
        simulate_config: RpcSimulateTransactionConfig,
    ) -> Result<TransactionOutcome> {
        if self.dry_run {
            let simulation_response = self
                .connection
                .simulate_transaction_with_config(transaction, simulate_config)
                .await?
                .value;

            let has_instruction_error = match &simulation_response.err {
                Some(tx_err) => {
                    ensure!(
                        matches!(tx_err, TransactionError::InstructionError(_, _)),
                        "Simulation failed: {tx_err}"
                    );
                    true
                }
                None => false,
            };

            if let Some(units_consumed) = &simulation_response.units_consumed {
                println!("Compute units consumed: {}", units_consumed);
            }

            println!("Simulated program logs:");
            simulation_response
                .logs
                .as_ref()
                .unwrap()
                .iter()
                .for_each(|log| {
                    println!("  {log}");
                });

            ensure!(!has_instruction_error, "Simulation failed");
            Ok(TransactionOutcome::Simulated(simulation_response))
        } else {
            let tx_sig = self
                .connection
                .send_and_confirm_transaction_with_spinner_and_config(
                    transaction,
                    self.connection.commitment(),
                    send_config,
                )
                .await?;

            Ok(TransactionOutcome::Executed(tx_sig))
        }
    }

    pub fn compute_units_for_bump_seed(bump: u8) -> u32 {
        1_500 * u32::from(255 - bump)
    }

    pub fn default_send_transaction_config(&self) -> RpcSendTransactionConfig {
        RpcSendTransactionConfig {
            preflight_commitment: Some(self.connection.commitment().commitment),
            ..Default::default()
        }
    }

    pub fn default_simulate_transaction_config(&self) -> RpcSimulateTransactionConfig {
        RpcSimulateTransactionConfig {
            commitment: Some(self.connection.commitment()),
            ..Default::default()
        }
    }
}

impl std::ops::Deref for Wallet {
    type Target = Keypair;

    fn deref(&self) -> &Self::Target {
        &self.signer
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
                ensure!(
                    payer_signer.pubkey() != signer.pubkey(),
                    "Specify fee payer if it differs from the main keypair"
                );

                Some(payer_signer)
            }
            None => None,
        };

        Ok(Wallet {
            connection: connection_options.into(),
            signer,
            compute_unit_price_ix: with_compute_unit_price
                .map(ComputeBudgetInstruction::set_compute_unit_price),
            verbose,
            fee_payer,
            dry_run,
        })
    }
}

fn try_load_specified_keypair(path: &PathBuf) -> Result<Keypair> {
    let keypair_file = std::fs::read_to_string(path)
        .with_context(|| format!("Keypair not found at {}", path.display()))?;
    let keypair_bytes = serde_json::from_str::<Vec<u8>>(&keypair_file)
        .with_context(|| format!("Keypair not valid JSON at {}", path.display()))?;
    let default_keypair = Keypair::try_from(keypair_bytes.as_slice())
        .with_context(|| format!("Invalid keypair found at {}", path.display()))?;

    Ok(default_keypair)
}
