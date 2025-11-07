use anyhow::{Result, anyhow, bail};
use doublezero_program_tools::{instruction::try_build_instruction, zero_copy};
use doublezero_revenue_distribution::{
    ID,
    instruction::{
        DistributionMerkleRootKind, RevenueDistributionInstructionData,
        account::{
            ConfigureDistributionDebtAccounts, FinalizeDistributionDebtAccounts,
            PaySolanaValidatorDebtAccounts, VerifyDistributionMerkleRootAccounts,
        },
    },
    state::Distribution,
    types::{DoubleZeroEpoch, SolanaValidatorDebt},
};
use doublezero_sdk::record::pubkey;
use doublezero_solana_client_tools::rpc::DoubleZeroLedgerConnection;
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
use svm_hash::merkle::MerkleProof;

use crate::{ledger, validator_debt::ComputedSolanaValidatorDebts};

#[derive(Debug)]
pub struct Transaction {
    pub signer: Keypair,
    pub dry_run: bool,
    pub force: bool,
}

#[derive(Debug, Serialize)]
pub struct DebtCollectionResults {
    pub collection_results: Vec<DebtCollectionResult>,
    pub total_transactions_attempted: usize,
    pub successful_transactions: usize,
    pub insufficient_funds: usize,
    pub already_paid: usize,
}

#[derive(Debug, Serialize)]
pub struct DebtCollectionResult {
    pub validator_id: String,
    pub amount: u64,
    pub result: Option<String>,
    pub success: bool,
}

impl Transaction {
    pub fn new(signer: Keypair, dry_run: bool, force: bool) -> Transaction {
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

        println!(
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

        println!("{:#?}", tx);

        Ok(())
    }

    pub async fn pay_solana_validator_debt(
        &self,
        solana_rpc_client: &RpcClient,
        debt: ComputedSolanaValidatorDebts,
        dz_epoch: u64,
    ) -> Result<DebtCollectionResults> {
        let mut debt_collection_result: Vec<DebtCollectionResult> =
            Vec::with_capacity(debt.debts.len());

        let debt_clone = debt.clone();
        for d in debt.debts {
            let debt_proof = debt_clone.find_debt_proof(&d.node_id).unwrap();
            let (_, proof) = debt_proof;
            let instruction = try_build_instruction(
                &ID,
                PaySolanaValidatorDebtAccounts::new(DoubleZeroEpoch::new(dz_epoch), &d.node_id),
                &RevenueDistributionInstructionData::PaySolanaValidatorDebt {
                    amount: d.amount,
                    proof,
                },
            )
            .unwrap();

            let recent_blockhash = solana_rpc_client.get_latest_blockhash().await?;
            let message =
                Message::try_compile(&self.signer.pubkey(), &[instruction], &[], recent_blockhash)
                    .unwrap();

            let versioned_transaction =
                VersionedTransaction::try_new(VersionedMessage::V0(message), &[&self.signer])
                    .unwrap();

            let result = self
                .send_or_simulate_transaction(solana_rpc_client, &versioned_transaction)
                .await;

            match result {
                Ok(success) => {
                    let payment_result = parse_program_logs(d.amount, d.node_id, success);
                    println!(
                        "{}: {:#?}",
                        payment_result.validator_id, payment_result.result
                    );
                    debt_collection_result.push(payment_result);
                }
                Err(err) => {
                    if let Some(client_error) = err.downcast_ref::<ClientError>() {
                        match &client_error.kind {
                            ClientErrorKind::RpcError(RpcError::RpcResponseError {
                                data:
                                    RpcResponseErrorData::SendTransactionPreflightFailure(sim_result),
                                ..
                            }) => {
                                if let Some(TransactionError::InstructionError(
                                    _instruction_code,
                                    _instruction_error,
                                )) = &sim_result.err
                                {
                                    let payment_result = DebtCollectionResult {
                                        amount: d.amount,
                                        validator_id: d.node_id.to_string(),
                                        result: if let Some(logs) = sim_result.logs.clone() {
                                            logs.get(4).cloned()
                                        } else {
                                            None
                                        },
                                        success: false,
                                    };
                                    println!(
                                        "{}: {:#?}",
                                        payment_result.validator_id, payment_result.result
                                    );
                                    debt_collection_result.push(payment_result);
                                }
                            }
                            _ => {
                                let counter = metrics::counter!("doublezero_validator_debt_pay_debt_transaction_failed", "client_error" => client_error.to_string());
                                counter.increment(1);
                                bail!("Unhandled Solana RPC error: {}", client_error);
                            }
                        }
                    }
                }
            }
        }

        let total_transactions = debt_collection_result.len();
        let total_success = debt_collection_result
            .iter()
            .filter(|pr| pr.success)
            .count();

        let insufficient_funds_count =
            count_failed_debt_collection("Insufficient funds", debt_collection_result.as_ref());
        let already_paid_count =
            count_failed_debt_collection("Merkle leaf", debt_collection_result.as_ref());

        let debt_collection_results = DebtCollectionResults {
            collection_results: debt_collection_result,
            total_transactions_attempted: total_transactions,
            successful_transactions: total_success,
            insufficient_funds: insufficient_funds_count,
            already_paid: already_paid_count,
        };
        Ok(debt_collection_results)
    }

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

fn count_failed_debt_collection(error_type: &str, dcr: &[DebtCollectionResult]) -> usize {
    dcr.iter()
        .filter(|pr| {
            pr.result
                .as_ref()
                .is_some_and(|res| res.contains(error_type))
        })
        .count()
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
