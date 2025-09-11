use anyhow::{Result, anyhow};
use doublezero_program_tools::{instruction::try_build_instruction, zero_copy};
use doublezero_revenue_distribution::{
    ID,
    instruction::{
        DistributionMerkleRootKind, RevenueDistributionInstructionData,
        account::{
            ConfigureDistributionDebtAccounts, FinalizeDistributionDebtAccounts,
            InitializeDistributionAccounts, PaySolanaValidatorDebtAccounts,
            VerifyDistributionMerkleRootAccounts,
        },
    },
    state::{Distribution, ProgramConfig},
    types::{DoubleZeroEpoch, SolanaValidatorDebt},
};
use solana_client::{
    nonblocking::rpc_client::RpcClient,
    rpc_client::SerializableTransaction,
    rpc_response::{Response, RpcSimulateTransactionResult},
};
use solana_sdk::{
    message::{VersionedMessage, v0::Message},
    pubkey::Pubkey,
    signature::{Keypair, Signature},
    signer::Signer,
    transaction::VersionedTransaction,
};
use std::env;
use svm_hash::merkle::MerkleProof;

use crate::validator_debt::ComputedSolanaValidatorDebts;

#[derive(Debug)]
pub struct Transaction {
    pub signer: Keypair,
    pub dry_run: bool,
}

fn mint_key() -> Pubkey {
    match env::var("MINT_KEY_ENVIRONMENT") {
        Ok(val) => {
            if val == "mainnet-beta" {
                doublezero_revenue_distribution::env::mainnet::DOUBLEZERO_MINT_KEY
            } else {
                doublezero_revenue_distribution::env::development::DOUBLEZERO_MINT_KEY
            }
        }
        Err(_) => doublezero_revenue_distribution::env::development::DOUBLEZERO_MINT_KEY,
    }
}

impl Transaction {
    pub fn new(signer: Keypair, dry_run: bool) -> Transaction {
        Transaction { signer, dry_run }
    }

    pub fn pubkey(&self) -> Pubkey {
        self.signer.pubkey()
    }

    pub async fn initialize_distribution(
        &self,
        solana_rpc_client: &RpcClient,
        fetched_dz_epoch: u64,
        dz_epoch: u64,
    ) -> Result<VersionedTransaction> {
        let keypair = self.signer.pubkey();
        let program_config_address = ProgramConfig::find_address().0;

        if fetched_dz_epoch != dz_epoch {
            anyhow::bail!("Fetched DZ epoch {fetched_dz_epoch} != parameter {dz_epoch}");
        }

        let account = solana_rpc_client
            .get_account(&program_config_address)
            .await?;
        let program_config =
            zero_copy::checked_from_bytes_with_discriminator::<ProgramConfig>(&account.data)
                .unwrap()
                .0;

        let initialize_distribution_ix = try_build_instruction(
            &ID,
            InitializeDistributionAccounts::new(
                &keypair,
                &keypair,
                program_config.next_dz_epoch,
                &mint_key(),
            ),
            &RevenueDistributionInstructionData::InitializeDistribution,
        )
        .unwrap();

        let recent_blockhash = solana_rpc_client.get_latest_blockhash().await?;
        let message = Message::try_compile(
            &keypair,
            &[initialize_distribution_ix],
            &[],
            recent_blockhash,
        )?;

        let new_transaction =
            VersionedTransaction::try_new(VersionedMessage::V0(message), &[&self.signer]).unwrap();
        Ok(new_transaction)
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
        dz_epoch: u64,
    ) -> Result<VersionedTransaction> {
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
    ) -> Result<Response<RpcSimulateTransactionResult>> {
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
        let verified = solana_rpc_client
            .simulate_transaction(&verified_transaction)
            .await?;
        Ok(verified)
    }

    pub async fn send_or_simulate_transaction(
        &self,
        solana_rpc_client: &RpcClient,
        transaction: &impl SerializableTransaction,
    ) -> Result<Option<Signature>> {
        if self.dry_run {
            let simulation_response = solana_rpc_client.simulate_transaction(transaction).await?;
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
            let tx_sig = solana_rpc_client
                .send_and_confirm_transaction(transaction)
                .await?;
            Ok(Some(tx_sig))
        }
    }

    pub async fn pay_solana_validator_debt(
        &self,
        solana_rpc_client: &RpcClient,
        debt: ComputedSolanaValidatorDebts,
        dz_epoch: u64,
    ) -> Result<Vec<VersionedTransaction>> {
        let mut transactions: Vec<VersionedTransaction> = Vec::new();
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
            transactions.push(versioned_transaction);
        }
        Ok(transactions)
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
    use solana_sdk::{commitment_config::CommitmentConfig, signer::Signer};

    use solana_transaction_status_client_types::{TransactionDetails, UiTransactionEncoding};
    use std::{path::PathBuf, str::FromStr, time::Duration};
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
        let transaction = Transaction::new(keypair, dry_run);
        let leaf = SolanaValidatorDebt {
            node_id: Pubkey::from_str("va1i6T6vTcijrCz6G8r89H6igKjwkLfF6g5fnpvZu1b").unwrap(),
            amount: 707,
        };

        let dz_epoch: u64 = 84;
        let record = ComputedSolanaValidatorDebts {
            epoch: 832,
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
    async fn test_initialize_distribution() -> anyhow::Result<()> {
        let keypair = try_load_keypair(None).unwrap();
        let commitment_config = CommitmentConfig::processed();
        let ledger_rpc_client =
            RpcClient::new_with_commitment("http://localhost:8899".to_string(), commitment_config);

        let solana_rpc_client =
            RpcClient::new_with_commitment("http://localhost:8899".to_string(), commitment_config);
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

        let transaction = Transaction::new(keypair, false);

        let dz_epoch_info = ledger_rpc_client.get_epoch_info().await?;

        let new_transaction = transaction
            .initialize_distribution(&ledger_rpc_client, dz_epoch_info.epoch, 85)
            .await?;

        let _sent_transaction = transaction
            .send_or_simulate_transaction(&solana_rpc_client, &new_transaction)
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

        let transaction = Transaction::new(keypair, false);

        let dz_epoch: u64 = 0;
        let finalize_transaction = transaction
            .finalize_distribution(&solana_rpc_client, dz_epoch)
            .await?;

        let _sent_transaction = transaction
            .send_or_simulate_transaction(&solana_rpc_client, &finalize_transaction)
            .await?;
        Ok(())
    }

    #[ignore = "needs local validator"]
    #[tokio::test]
    async fn test_write_to_read_from_chain() -> anyhow::Result<()> {
        let keypair = try_load_keypair(None).unwrap();
        let k = keypair.pubkey();
        let validator_id = "devgM7SXHvoHH6jPXRsjn97gygPUo58XEnc9bqY1jpj";
        let commitment_config = CommitmentConfig::processed();
        let ledger_rpc_client =
            RpcClient::new_with_commitment("http://localhost:8899".to_string(), commitment_config);

        let solana_rpc_client =
            RpcClient::new_with_commitment("http://localhost:8899".to_string(), commitment_config);
        let vote_account_config = RpcGetVoteAccountsConfig {
            vote_pubkey: Some(validator_id.to_string()),
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

        let tx_sig = solana_rpc_client
            .request_airdrop(&k, 1_000_000_000)
            .await
            .unwrap();

        while !solana_rpc_client
            .confirm_transaction_with_commitment(&tx_sig, commitment_config)
            .await
            .unwrap()
            .value
        {
            tokio::time::sleep(Duration::from_millis(400)).await;
        }

        // Make sure airdrop went through.
        while solana_rpc_client
            .get_balance_with_commitment(&k, commitment_config)
            .await
            .unwrap()
            .value
            == 0
        {
            // Airdrop doesn't get processed after a slot unfortunately.
            tokio::time::sleep(Duration::from_secs(2)).await;
        }

        let transaction = Transaction::new(keypair, false);

        let new_transaction = transaction
            .initialize_distribution(&solana_rpc_client, 0, 0)
            .await?;

        let _sent_transaction = transaction
            .send_or_simulate_transaction(&solana_rpc_client, &new_transaction)
            .await?;

        let debt = RevenueDistributionInstructionData::ConfigureDistributionDebt {
            total_validators: 5,
            total_debt: 100_000,
            merkle_root: Hash::from_str("7biGoeW59qKyVEqL2iWAm6H4hhRCExk6LxbgGrpXptci").unwrap(),
        };

        let dz_epoch = 0;
        let t = transaction
            .submit_distribution(&solana_rpc_client, dz_epoch, debt)
            .await?;

        let _tr = transaction
            .send_or_simulate_transaction(&solana_rpc_client, &t)
            .await?;

        let _rt = transaction.read_distribution(0, &solana_rpc_client).await?;

        Ok(())
    }
}
