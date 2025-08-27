use anyhow::{Result, anyhow};
use doublezero_program_tools::{instruction::try_build_instruction, zero_copy};

use doublezero_revenue_distribution::{
    ID,
    // TODO: add env var to load correct key
    instruction::{
        account::{ConfigureDistributionPaymentsAccounts, InitializeDistributionAccounts},
        {DistributionPaymentsConfiguration, RevenueDistributionInstructionData},
    },
    state::{Distribution, ProgramConfig},
    types::DoubleZeroEpoch,
};
use solana_client::{nonblocking::rpc_client::RpcClient, rpc_client::SerializableTransaction};
use solana_sdk::{
    message::{VersionedMessage, v0::Message},
    pubkey::Pubkey,
    signature::{Keypair, Signature},
    signer::Signer,
    transaction::VersionedTransaction,
};
use std::env;

#[derive(Debug)]
pub struct Transaction {
    signer: Keypair,

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
    // TODO: check against DZ ledger
    pub async fn initialize_distribution(
        &self,
        ledger_rpc_client: &RpcClient,
        solana_rpc_client: &RpcClient,
        dz_epoch: u64,
    ) -> Result<VersionedTransaction> {
        let keypair = self.signer.pubkey();
        let program_config_address = ProgramConfig::find_address().0;
        let fetched_dz_epoch_info = ledger_rpc_client.get_epoch_info().await?;
        let fetched_dz_epoch = fetched_dz_epoch_info.epoch;

        if fetched_dz_epoch >= dz_epoch {
            anyhow::bail!("Fetched DZ epoch {fetched_dz_epoch} >= parameter {dz_epoch}");
        }

        let account = solana_rpc_client
            .get_account(&program_config_address)
            .await?;
        let program_config =
            zero_copy::checked_from_bytes_with_discriminator::<ProgramConfig>(&account.data)
                .unwrap()
                .0;

        // TODO: derive mint_key from env var
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
        debts: DistributionPaymentsConfiguration,
    ) -> Result<VersionedTransaction> {
        let doublezero_epoch = DoubleZeroEpoch::new(dz_epoch);
        match try_build_instruction(
            &ID,
            ConfigureDistributionPaymentsAccounts::new(&self.signer.pubkey(), doublezero_epoch),
            &RevenueDistributionInstructionData::ConfigureDistributionPayments(debts),
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
            Err(err) => Err(anyhow!("Failed to build distribution: {err:?}")),
        }
    }

    pub async fn send_or_simulate_transaction(
        &self,
        rpc_client: &RpcClient,
        transaction: &impl SerializableTransaction,
    ) -> Result<Option<Signature>> {
        if self.dry_run {
            let simulation_response = rpc_client.simulate_transaction(transaction).await?;
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
            let tx_sig = rpc_client.send_and_confirm_transaction(transaction).await?;
            Ok(Some(tx_sig))
        }
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
    use crate::fee_payment_calculator::FeePaymentCalculator;
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

    #[ignore] // this will fail without local validator
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
        let fpc = FeePaymentCalculator::new(
            ledger_rpc_client,
            solana_rpc_client,
            rpc_block_config,
            vote_account_config,
        );
        let solana_rpc_client = fpc.solana_rpc_client;
        let ledger_rpc_client = fpc.ledger_rpc_client;

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
            .initialize_distribution(&ledger_rpc_client, &solana_rpc_client, 0)
            .await?;

        let _sent_transaction = transaction
            .send_or_simulate_transaction(&solana_rpc_client, &new_transaction)
            .await?;

        let debt = DistributionPaymentsConfiguration::UpdateSolanaValidatorPayments {
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
