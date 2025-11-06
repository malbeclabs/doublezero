use crate::{ledger, validator_debt::ComputedSolanaValidatorDebts};
use anyhow::{Result, anyhow, bail};
use borsh::BorshDeserialize;
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
pub const SOLANA_SEED_PREFIX: &[u8; 21] = b"solana_validator_debt";

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
        dz_ledger_rpc_client: &RpcClient,
        dz_epoch: u64,
    ) -> Result<VersionedTransaction> {
        let dz_epoch_bytes = dz_epoch.to_le_bytes();
        let seeds: &[&[u8]] = &[SOLANA_SEED_PREFIX, &dz_epoch_bytes];

        let read = ledger::read_from_ledger(
            dz_ledger_rpc_client,
            &self.signer,
            seeds,
            dz_ledger_rpc_client.commitment(),
        )
        .await?;

        let deserialized = ComputedSolanaValidatorDebts::try_from_slice(read.1.as_slice())?;

        for debt_entry in deserialized.clone().debts {
            let debt_proof = deserialized.find_debt_proof(&debt_entry.node_id).unwrap();
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
        let seed: &[&[u8]] = &[SOLANA_SEED_PREFIX, &dz_epoch_bytes];
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

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        solana_debt_calculator::{SolanaDebtCalculator, ledger_rpc, solana_rpc},
        validator_debt::{ComputedSolanaValidatorDebt, ComputedSolanaValidatorDebts},
    };

    use solana_client::{
        nonblocking::rpc_client::RpcClient,
        rpc_config::{RpcBlockConfig, RpcGetVoteAccountsConfig},
    };
    use solana_sdk::commitment_config::CommitmentConfig;

    use solana_transaction_status_client_types::{TransactionDetails, UiTransactionEncoding};
    use std::{path::PathBuf, str::FromStr};
    use svm_hash::sha2::Hash;

    /// Taken from a Solana cookbook to load a keypair from a user's Solana config
    /// location.
    fn try_load_keypair(path: Option<PathBuf>) -> Result<Keypair> {
        let home_path = std::env::var_os("HOME").unwrap();
        let default_keypair_path = ".config/solana/id.json";

        let keypair_path =
            path.unwrap_or_else(|| PathBuf::from(home_path).join(default_keypair_path));
        try_load_specified_keypair(&keypair_path)
    }

    fn try_load_specified_keypair(path: &PathBuf) -> Result<Keypair> {
        let keypair_file = std::fs::read_to_string(path)?;
        let keypair_bytes = serde_json::from_str::<Vec<u8>>(&keypair_file)?;
        let default_keypair = Keypair::try_from(keypair_bytes.as_slice())?;

        Ok(default_keypair)
    }

    #[ignore = "needs local validator"]
    #[tokio::test]
    async fn test_verify_merkle_root() -> anyhow::Result<()> {
        let keypair = try_load_keypair(None).unwrap();
        let commitment_config = CommitmentConfig::processed();
        let ledger_rpc_client = RpcClient::new_with_commitment(ledger_rpc(), commitment_config);

        let solana_rpc_client = RpcClient::new_with_commitment(solana_rpc(), commitment_config);
        let vote_account_config = RpcGetVoteAccountsConfig {
            vote_pubkey: None,
            commitment: CommitmentConfig::finalized().into(),
            keep_unstaked_delinquents: None,
            delinquent_slot_distance: None,
        };

        let rpc_block_config = RpcBlockConfig {
            encoding: Some(UiTransactionEncoding::Base58),
            transaction_details: Some(TransactionDetails::None),
            rewards: Some(true),
            commitment: None,
            max_supported_transaction_version: Some(0),
        };
        let fpc = SolanaDebtCalculator::new(
            ledger_rpc_client,
            solana_rpc_client,
            rpc_block_config,
            vote_account_config,
        );
        let solana_rpc_client = fpc.solana_rpc_client;
        let dry_run = true;
        let force = false;
        let transaction = Transaction::new(keypair, dry_run, force);
        let leaf = SolanaValidatorDebt {
            node_id: Pubkey::from_str("va1i6T6vTcijrCz6G8r89H6igKjwkLfF6g5fnpvZu1b").unwrap(),
            amount: 707,
        };

        let dz_epoch: u64 = 84;
        let record = ComputedSolanaValidatorDebts {
            blockhash: Hash::new_unique(),
            first_solana_epoch: 832,
            last_solana_epoch: 832,
            debts: vec![ComputedSolanaValidatorDebt {
                node_id: Pubkey::from_str("va1i6T6vTcijrCz6G8r89H6igKjwkLfF6g5fnpvZu1b").unwrap(),
                amount: 707,
            }],
        };
        let debt_proof = record.find_debt_proof(
            &Pubkey::from_str("va1i6T6vTcijrCz6G8r89H6igKjwkLfF6g5fnpvZu1b").unwrap(),
        );
        let (_, proof) = debt_proof.unwrap();
        transaction
            .verify_merkle_root(&solana_rpc_client, dz_epoch, proof, leaf)
            .await?;

        Ok(())
    }

    #[ignore = "needs local validator"]
    #[tokio::test]
    async fn test_finalize_distribution() -> anyhow::Result<()> {
        let keypair = try_load_keypair(None).unwrap();
        let commitment_config = CommitmentConfig::processed();
        let ledger_rpc_client = RpcClient::new_with_commitment(ledger_rpc(), commitment_config);

        let solana_rpc_client = RpcClient::new_with_commitment(solana_rpc(), commitment_config);
        let vote_account_config = RpcGetVoteAccountsConfig {
            vote_pubkey: None,
            commitment: CommitmentConfig::finalized().into(),
            keep_unstaked_delinquents: None,
            delinquent_slot_distance: None,
        };

        let rpc_block_config = RpcBlockConfig {
            encoding: Some(UiTransactionEncoding::Base58),
            transaction_details: Some(TransactionDetails::None),
            rewards: Some(true),
            commitment: None,
            max_supported_transaction_version: Some(0),
        };
        let fpc = SolanaDebtCalculator::new(
            ledger_rpc_client,
            solana_rpc_client,
            rpc_block_config,
            vote_account_config,
        );
        let solana_rpc_client = fpc.solana_rpc_client;
        let ledger_rpc_client = fpc.ledger_rpc_client;

        let transaction = Transaction::new(keypair, false, false);

        let dz_epoch: u64 = 0;
        let finalize_transaction = transaction
            .finalize_distribution(&solana_rpc_client, &ledger_rpc_client, dz_epoch)
            .await?;

        let _sent_transaction = transaction
            .send_or_simulate_transaction(&solana_rpc_client, &finalize_transaction)
            .await?;
        Ok(())
    }
}
