use std::{fs::File, sync::Arc};

use anyhow::{Result, anyhow};
use doublezero_sdk::record::pubkey;
use doublezero_solana_client_tools::{
    account::zero_copy::ZeroCopyAccountOwnedData, rpc::DoubleZeroLedgerConnection,
};
use doublezero_solana_sdk::{
    merkle::MerkleProof,
    revenue_distribution::{
        ID,
        instruction::{
            DistributionMerkleRootKind, RevenueDistributionInstructionData,
            account::{
                ConfigureDistributionDebtAccounts, FinalizeDistributionDebtAccounts,
                PaySolanaValidatorDebtAccounts, VerifyDistributionMerkleRootAccounts,
            },
        },
        state::Distribution,
        try_is_processed_leaf,
        types::{DoubleZeroEpoch, SolanaValidatorDebt},
    },
    try_build_instruction, zero_copy,
};
use futures::stream::{self, StreamExt};
use serde::Serialize;
use solana_client::{
    client_error::{ClientError, ClientErrorKind},
    nonblocking::rpc_client::RpcClient,
    rpc_client::SerializableTransaction,
    rpc_request::{RpcError, RpcResponseErrorData},
};
use solana_sdk::{
    hash::Hash,
    message::{VersionedMessage, v0::Message},
    pubkey::Pubkey,
    signature::Keypair,
    signer::Signer,
    transaction::{TransactionError, VersionedTransaction},
};
use tokio::sync::Semaphore;

use crate::{
    ledger,
    validator_debt::{ComputedSolanaValidatorDebt, ComputedSolanaValidatorDebts},
};

const MAX_CONCURRENT_CONNECTIONS: usize = 10;

#[derive(Debug)]
pub struct Transaction {
    pub signer: Arc<Keypair>,
    pub dry_run: bool,
    pub force: bool,
}

#[derive(Clone, Debug, Serialize)]
pub struct DebtCollectionResults {
    pub collection_results: Vec<DebtCollectionResult>,
    pub dz_epoch: u64,
    pub successful_transactions_count: usize,
    pub insufficient_funds_count: usize,
    pub already_paid_count: usize,
    pub total_debt: u64,
    pub total_paid: u64,
    pub already_paid: u64,
    pub total_validators: usize,
}

#[derive(Clone, Debug, Serialize)]
pub struct DebtCollectionResult {
    pub validator_id: String,
    pub amount: u64,
    pub result: Option<String>,
    pub success: bool,
}

impl Transaction {
    pub fn new(signer: Arc<Keypair>, dry_run: bool, force: bool) -> Transaction {
        Transaction {
            signer,
            dry_run,
            force,
        }
    }

    pub fn pubkey(&self) -> Pubkey {
        self.signer.pubkey()
    }

    pub async fn submit_distribution(
        &self,
        solana_rpc_client: &RpcClient,
        dz_epoch: u64,
        debts: RevenueDistributionInstructionData,
    ) -> Result<VersionedTransaction> {
        let doublezero_epoch = DoubleZeroEpoch::new(dz_epoch);
        match try_build_instruction(
            &ID,
            ConfigureDistributionDebtAccounts::new(&self.signer.pubkey(), doublezero_epoch),
            &debts,
        ) {
            Ok(instruction) => {
                let recent_blockhash = solana_rpc_client.get_latest_blockhash().await?;
                let message = Message::try_compile(
                    &self.signer.pubkey(),
                    &[instruction],
                    &[],
                    recent_blockhash,
                )
                .unwrap();

                let new_transaction =
                    VersionedTransaction::try_new(VersionedMessage::V0(message), &[&self.signer])
                        .unwrap();
                Ok(new_transaction)
            }
            Err(err) => Err(anyhow!(
                "Failed to build initialize distribution instruction: {err:?}"
            )),
        }
    }

    pub async fn finalize_distribution(
        &self,
        solana_rpc_client: &RpcClient,
        dz_connection: &DoubleZeroLedgerConnection,
        dz_epoch: u64,
    ) -> Result<VersionedTransaction> {
        let (_, computed_debt) = ledger::try_fetch_debt_record(
            dz_connection,
            &self.signer.pubkey(),
            dz_epoch,
            dz_connection.commitment(),
        )
        .await?;

        for debt_entry in computed_debt.debts.iter() {
            let debt_proof = computed_debt.find_debt_proof(&debt_entry.node_id).unwrap();
            let (_, proof) = debt_proof;

            let leaf = SolanaValidatorDebt {
                node_id: debt_entry.node_id,
                amount: debt_entry.amount,
            };

            self.verify_merkle_root(solana_rpc_client, dz_epoch, proof, leaf)
                .await?;
        }

        let dz_epoch = DoubleZeroEpoch::new(dz_epoch);

        match try_build_instruction(
            &ID,
            FinalizeDistributionDebtAccounts::new(&self.pubkey(), dz_epoch, &self.pubkey()),
            &RevenueDistributionInstructionData::FinalizeDistributionDebt,
        ) {
            Ok(instruction) => {
                let recent_blockhash = solana_rpc_client.get_latest_blockhash().await?;
                let message = Message::try_compile(
                    &self.signer.pubkey(),
                    &[instruction],
                    &[],
                    recent_blockhash,
                )
                .unwrap();

                let finalized_transaction =
                    VersionedTransaction::try_new(VersionedMessage::V0(message), &[&self.signer])
                        .unwrap();
                Ok(finalized_transaction)
            }
            Err(err) => Err(anyhow!(
                "Failed to build finalize distribution instruction: {err:?}"
            )),
        }
    }

    // only simulate transaction
    pub async fn verify_merkle_root(
        &self,
        solana_rpc_client: &RpcClient,
        dz_epoch: u64,
        proof: MerkleProof,
        leaf: SolanaValidatorDebt,
    ) -> Result<()> {
        let dz_epoch = DoubleZeroEpoch::new(dz_epoch);
        let instruction = try_build_instruction(
            &ID,
            VerifyDistributionMerkleRootAccounts::new(dz_epoch),
            &RevenueDistributionInstructionData::VerifyDistributionMerkleRoot {
                kind: DistributionMerkleRootKind::SolanaValidatorDebt(leaf),
                proof,
            },
        )?;

        let recent_blockhash = solana_rpc_client.get_latest_blockhash().await?;
        let message =
            Message::try_compile(&self.signer.pubkey(), &[instruction], &[], recent_blockhash)
                .unwrap();

        let verified_transaction =
            VersionedTransaction::try_new(VersionedMessage::V0(message), &[&self.signer])
                .map_err(|e| anyhow!("Failed to create verified instruction: {e:?}"))?;
        let verification = solana_rpc_client
            .simulate_transaction(&verified_transaction)
            .await?;
        anyhow::ensure!(
            verification.value.err.is_none(),
            "simulation verification failed"
        );

        tracing::info!(
            "Verification Result: {:#?}",
            verification.value.logs.unwrap_or(Vec::new())
        );

        Ok(())
    }

    pub async fn send_or_simulate_transaction(
        &self,
        solana_rpc_client: &RpcClient,
        transaction: &impl SerializableTransaction,
    ) -> Result<Option<String>> {
        if self.dry_run {
            let simulation_response = solana_rpc_client.simulate_transaction(transaction).await?;
            Ok(Some(simulation_response.value.logs.unwrap().join("\n ")))
        } else {
            let tx_sig = solana_rpc_client
                .send_and_confirm_transaction(transaction)
                .await?;
            Ok(Some(tx_sig.to_string()))
        }
    }

    pub async fn close_account(
        &self,
        ledger_rpc_client: &RpcClient,
        dz_epoch: u64,
        recent_blockhash: Hash,
    ) -> Result<()> {
        let dz_epoch_bytes = dz_epoch.to_le_bytes();
        let seed = &[
            ComputedSolanaValidatorDebts::RECORD_SEED_PREFIX,
            &dz_epoch_bytes,
        ];
        let key = pubkey::create_record_key(&self.pubkey(), seed);
        let instruction =
            doublezero_record::instruction::close_account(&key, &self.pubkey(), &self.pubkey());

        let message =
            Message::try_compile(&self.pubkey(), &[instruction], &[], recent_blockhash).unwrap();

        let verified_transaction =
            VersionedTransaction::try_new(VersionedMessage::V0(message), &[&self.signer])
                .map_err(|e| anyhow::anyhow!("Failed to create verified instruction: {e:?}"))?;

        let tx = &self
            .send_or_simulate_transaction(ledger_rpc_client, &verified_transaction)
            .await?;

        tracing::info!("{:#?}", tx);
        Ok(())
    }

    pub async fn pay_solana_validator_debt(
        &self,
        solana_rpc_client: &RpcClient,
        debt: ComputedSolanaValidatorDebts,
        dz_epoch: u64,
        distribution: &ZeroCopyAccountOwnedData<Distribution>,
    ) -> Result<DebtCollectionResults> {
        let mut overrides = Vec::new();
        // TODO: This is a temporary fix to exclude a couple of validators
        // the longer term fix will be using data on-chain as it's more transparent, less error-prone
        if let Ok(file) = File::open("/opt/doublezero-offchain-scheduler/overrides.csv") {
            let mut rdr = csv::Reader::from_reader(file);
            overrides.extend(
                rdr.records()
                    .filter_map(|result| result.ok())
                    .filter_map(|record| {
                        let pubkey = record.get(0)?;
                        let epoch = record.get(1)?.parse::<u64>().ok()?;
                        Some((pubkey.to_string(), epoch))
                    }),
            );
        }
        let debts_to_process: Vec<ComputedSolanaValidatorDebt> = debt.debts.iter().filter(|debt| {
          let node_id_str = debt.node_id.to_string();
          let excluded = overrides.iter().any(|(key, epoch)| key == &node_id_str && *epoch == dz_epoch);
      if excluded {
          tracing::info!(
                            "Validator {node_id_str} for epoch #{dz_epoch} excluded from debt collection"
                        );

      }
      !excluded

        }).cloned().collect();

        let start_index = distribution.processed_solana_validator_debt_start_index as usize;
        let end_index = distribution.processed_solana_validator_debt_end_index as usize;
        let processed_leaf_data = &distribution.remaining_data[start_index..end_index];

        let semaphore = Arc::new(Semaphore::new(MAX_CONCURRENT_CONNECTIONS));
        let debt_clone = Arc::new(debt);

        let debt_collection_results: Vec<Result<DebtCollectionResult>> =
            stream::iter(debts_to_process)
                .map(|debt| {
                    let semaphore = semaphore.clone();
                    let debt_clone = debt_clone.clone();

                    let debt_proof = debt_clone.find_debt_proof(&debt.node_id).unwrap();
                    let (_, proof) = debt_proof;
                    let leaf_index = proof.leaf_index.unwrap() as usize;

                    async move {
                        let _permit = semaphore
                            .acquire()
                            .await
                            .map_err(|e| anyhow!("Semaphore error: {}", e))?;

                        if try_is_processed_leaf(processed_leaf_data, leaf_index).unwrap() {
                            Ok(DebtCollectionResult {
                                validator_id: debt.node_id.to_string(),
                                amount: debt.amount,
                                result: Some("Merkle leaf".to_string()),
                                success: false,
                            })
                        } else {
                            Self::process_single_debt_payment(
                                self,
                                solana_rpc_client,
                                &debt,
                                proof,
                                dz_epoch,
                            )
                            .await
                        }
                    }
                })
                .buffer_unordered(20)
                .collect()
                .await;

        let mut debt_collection_result: Vec<DebtCollectionResult> =
            Vec::with_capacity(debt_collection_results.len());

        for result in debt_collection_results {
            match result {
                Ok(payment_result) => {
                    debt_collection_result.push(payment_result);
                }

                Err(err) => {
                    eprintln!("Error processing debt payment: {}", err);
                }
            }
        }

        let mut successful_transactions_count = 0;
        let mut successful_transactions_amount = 0;
        let mut already_paid_count = 0;
        let mut already_paid = 0;
        let mut insufficient_funds_count = 0;
        let mut total_debt: u64 = 0;

        for dcr in &debt_collection_result {
            total_debt += dcr.amount;
            if dcr.success {
                successful_transactions_count += 1;
                successful_transactions_amount += dcr.amount;
            } else if let Some(result_str) = &dcr.result {
                if result_str.contains("Merkle leaf") {
                    // already paid
                    already_paid_count += 1;
                    already_paid += dcr.amount;
                } else if result_str.contains("Insufficient funds") {
                    insufficient_funds_count += 1;
                }
            }
        }
        let total_validators = debt_collection_result.len();
        let total_paid = already_paid + successful_transactions_amount;

        let debt_collection_results = DebtCollectionResults {
            collection_results: debt_collection_result,
            dz_epoch,
            successful_transactions_count,
            insufficient_funds_count,
            already_paid_count,
            already_paid,
            total_debt,
            total_paid,
            total_validators,
        };
        Ok(debt_collection_results)
    }

    async fn process_single_debt_payment(
        transaction: &Transaction,
        solana_rpc_client: &RpcClient,
        debt: &ComputedSolanaValidatorDebt,
        proof: MerkleProof,
        dz_epoch: u64,
    ) -> Result<DebtCollectionResult> {
        let instruction = try_build_instruction(
            &ID,
            PaySolanaValidatorDebtAccounts::new(DoubleZeroEpoch::new(dz_epoch), &debt.node_id),
            &RevenueDistributionInstructionData::PaySolanaValidatorDebt {
                amount: debt.amount,
                proof,
            },
        )
        .unwrap();

        let recent_blockhash = solana_rpc_client.get_latest_blockhash().await?;

        let message = Message::try_compile(
            &transaction.signer.pubkey(),
            &[instruction],
            &[],
            recent_blockhash,
        )
        .unwrap();

        let versioned_transaction =
            VersionedTransaction::try_new(VersionedMessage::V0(message), &[&transaction.signer])
                .unwrap();

        let result = Self::send_or_simulate_transaction(
            transaction,
            solana_rpc_client,
            &versioned_transaction,
        )
        .await;

        match result {
            Ok(success) => {
                let payment_result = parse_program_logs(debt.amount, debt.node_id, success);
                Ok(payment_result)
            }
            Err(err) => {
                if let Some(client_error) = err.downcast_ref::<ClientError>() {
                    match &client_error.kind {
                        ClientErrorKind::RpcError(RpcError::RpcResponseError {
                            data: RpcResponseErrorData::SendTransactionPreflightFailure(sim_result),
                            ..
                        }) => {
                            if let Some(TransactionError::InstructionError(
                                _instruction_code,
                                _instruction_error,
                            )) = &sim_result.err
                            {
                                let payment_result = DebtCollectionResult {
                                    amount: debt.amount,
                                    validator_id: debt.node_id.to_string(),
                                    result: if let Some(logs) = sim_result.logs.clone() {
                                        logs.get(4).cloned()
                                    } else {
                                        None
                                    },
                                    success: false,
                                };
                                Ok(payment_result)
                            } else {
                                Err(err)
                            }
                        }
                        _ => {
                            let counter = metrics::counter!("doublezero_validator_debt_pay_debt_transaction_failed", "client_error" => client_error.to_string());
                            counter.increment(1);
                            Err(err)
                        }
                    }
                } else {
                    Err(err)
                }
            }
        }
    }

    // TODO: Get rid of this because only one thing calls it.
    pub async fn read_distribution(
        &self,
        dz_epoch: u64,
        rpc_client: &RpcClient,
    ) -> Result<Distribution> {
        let (distribution_key, _bump) = Distribution::find_address(DoubleZeroEpoch::new(dz_epoch));
        let distribution_account = rpc_client.get_account(&distribution_key).await?;

        let distribution_state = zero_copy::checked_from_bytes_with_discriminator::<Distribution>(
            &distribution_account.data,
        )
        .expect("Failed to deserialize Distribution account data.")
        .0;

        Ok(*distribution_state)
    }
}

fn parse_program_logs(
    amount: u64,
    node_id: Pubkey,
    program_logs: Option<String>,
) -> DebtCollectionResult {
    let parsed_data = program_logs.as_ref().map(|logs| {
        let success_or_fail_line = logs.lines().nth(4);

        // should no longer see uninitialized account errors
        let success = success_or_fail_line
            .map(|line| !line.contains("Merkle leaf") && !line.contains("Insufficient funds"))
            .unwrap_or(true);

        (success, success_or_fail_line.map(String::from))
    });

    let (success, result) = parsed_data.unwrap_or((true, None));

    DebtCollectionResult {
        amount,
        validator_id: node_id.to_string(),
        result,
        success,
    }
}
