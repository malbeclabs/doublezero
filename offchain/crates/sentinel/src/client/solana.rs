use crate::{AccessId, Error, Result, new_transaction};
use base64::{Engine, engine::general_purpose::STANDARD as BASE64_STD};
use bincode;
use doublezero_passport::{
    id as passport_id,
    instruction::{
        PassportInstructionData,
        account::{DenyAccessAccounts, GrantAccessAccounts},
    },
    state::AccessRequest,
};
use doublezero_program_tools::{
    Discriminator, PrecomputedDiscriminator, instruction::try_build_instruction, zero_copy,
};
use futures::{future::BoxFuture, stream::BoxStream};
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::{
    nonblocking::{pubsub_client::PubsubClient, rpc_client::RpcClient},
    rpc_config::{
        RpcAccountInfoConfig, RpcLeaderScheduleConfig, RpcProgramAccountsConfig,
        RpcTransactionConfig, RpcTransactionLogsConfig, RpcTransactionLogsFilter,
    },
    rpc_filter::{Memcmp, RpcFilterType},
    rpc_response::{Response, RpcLogsResponse},
};
use solana_commitment_config::{CommitmentConfig, CommitmentLevel};
use solana_sdk::{
    compute_budget::ComputeBudgetInstruction,
    instruction::CompiledInstruction,
    pubkey::Pubkey,
    signature::{Keypair, Signature},
    signer::Signer,
    transaction::VersionedTransaction,
};
use solana_transaction_status_client_types::{
    EncodedTransaction, TransactionBinaryEncoding, UiTransactionEncoding,
};
use std::{
    net::{Ipv4Addr, SocketAddr},
    sync::Arc,
};
use url::Url;

const ACCESS_REQUEST_ACCOUNT_INDEX: usize = 2;

pub struct SolRpcClient {
    client: RpcClient,
    payer: Arc<Keypair>,
}

impl SolRpcClient {
    pub fn new(rpc_url: Url, payer: Arc<Keypair>) -> Self {
        Self {
            client: RpcClient::new_with_commitment(rpc_url.into(), CommitmentConfig::confirmed()),
            payer,
        }
    }

    pub async fn grant_access(
        &self,
        access_request_key: &Pubkey,
        rent_beneficiary_key: &Pubkey,
    ) -> Result<Signature> {
        let signer = &self.payer;
        let grant_ix = try_build_instruction(
            &passport_id(),
            GrantAccessAccounts::new(&signer.pubkey(), access_request_key, rent_beneficiary_key),
            &PassportInstructionData::GrantAccess,
        )?;

        let recent_blockhash = self.client.get_latest_blockhash().await?;

        // There should be ~5k CU buffer with this limit.
        let compute_limit_ix = ComputeBudgetInstruction::set_compute_unit_limit(16_000);

        // TODO: Consider using a priority fee API instead of a fixed price.
        let compute_price_ix = ComputeBudgetInstruction::set_compute_unit_price(100_000);

        let transaction = new_transaction(
            &[grant_ix, compute_limit_ix, compute_price_ix],
            &[signer],
            recent_blockhash,
        );

        Ok(self
            .client
            .send_and_confirm_transaction(&transaction)
            .await?)
    }

    pub async fn deny_access(&self, access_request_key: &Pubkey) -> Result<Signature> {
        let signer = &self.payer;
        let deny_ix = try_build_instruction(
            &passport_id(),
            DenyAccessAccounts::new(&signer.pubkey(), access_request_key),
            &PassportInstructionData::DenyAccess,
        )?;

        // There should be ~5k CU buffer with this limit.
        let compute_limit_ix = ComputeBudgetInstruction::set_compute_unit_limit(12_000);

        // TODO: Consider using a priority fee API instead of a fixed price.
        let compute_price_ix = ComputeBudgetInstruction::set_compute_unit_price(100_000);

        let recent_blockhash = self.client.get_latest_blockhash().await?;

        let transaction = new_transaction(
            &[deny_ix, compute_limit_ix, compute_price_ix],
            &[signer],
            recent_blockhash,
        );

        Ok(self
            .client
            .send_and_confirm_transaction(&transaction)
            .await?)
    }

    pub async fn get_access_requests_from_signature(
        &self,
        signature: Signature,
    ) -> Result<Vec<AccessId>> {
        // Get the transaction to find the AccessRequest account pubkey
        let txn = self
            .client
            .get_transaction_with_config(
                &signature,
                RpcTransactionConfig {
                    encoding: Some(UiTransactionEncoding::Base64),
                    commitment: Some(CommitmentConfig {
                        commitment: CommitmentLevel::Confirmed,
                    }),
                    max_supported_transaction_version: Some(0),
                },
            )
            .await?;

        let mut access_ids = Vec::new();

        if let EncodedTransaction::Binary(data, TransactionBinaryEncoding::Base64) =
            txn.transaction.transaction
        {
            let data = BASE64_STD.decode(data)?;
            let tx = bincode::deserialize::<VersionedTransaction>(&data)?;

            let static_account_keys = tx.message.static_account_keys();
            let instructions = tx.message.instructions();

            for compiled_ix in instructions
                .iter()
                .filter(|ix| is_request_access_instruction(ix, static_account_keys))
            {
                // Get the AccessRequest account
                let accounts = compiled_ix
                    .accounts
                    .iter()
                    .map(|&idx| static_account_keys.get(idx as usize))
                    .collect::<Option<Vec<_>>>()
                    .ok_or(Error::MissingAccountKeys(signature))?;

                let request_pda = accounts
                    .get(ACCESS_REQUEST_ACCOUNT_INDEX)
                    .copied()
                    .ok_or(Error::InstructionInvalid(signature))?;

                // Fetch the AccessRequest account data
                let account = self.client.get_account(request_pda).await?;

                // Deserialize the AccessRequest and extract the AccessMode
                let access_id =
                    deserialize_access_request_from_account(request_pda, &account.data)?;

                access_ids.push(access_id);
            }
        } else {
            return Err(Error::TransactionEncoding(signature));
        };

        Ok(access_ids)
    }

    pub async fn get_access_requests(&self) -> Result<Vec<AccessId>> {
        let config = RpcProgramAccountsConfig {
            filters: Some(vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                0,
                AccessRequest::discriminator_slice().to_vec(),
            ))]),
            account_config: RpcAccountInfoConfig {
                encoding: Some(UiAccountEncoding::Base64),
                ..Default::default()
            },
            ..Default::default()
        };

        let accounts = self
            .client
            .get_program_accounts_with_config(&passport_id(), config)
            .await?;

        let access_ids = accounts
            .into_iter()
            .filter_map(|(pubkey, account)| {
                deserialize_access_request_from_account(&pubkey, &account.data).ok()
            })
            .collect();

        Ok(access_ids)
    }

    pub async fn check_leader_schedule(
        &self,
        validator_id: &Pubkey,
        previous_leader_epochs: u8,
    ) -> Result<bool> {
        let latest_slot = self.client.get_slot().await?;

        for slot in PreviousEpochSlots::new(latest_slot).take(previous_leader_epochs as usize) {
            let config = RpcLeaderScheduleConfig {
                identity: Some(validator_id.to_string()),
                ..Default::default()
            };

            if !self
                .client
                .get_leader_schedule_with_config(Some(slot), config)
                .await?
                .is_some_and(|schedule| schedule.is_empty())
            {
                return Ok(true);
            }
        }

        Ok(false)
    }

    pub async fn get_validator_ip(&self, validator_id: &Pubkey) -> Result<Option<Ipv4Addr>> {
        let address = self
            .client
            .get_cluster_nodes()
            .await?
            .iter()
            .find(|contact| contact.pubkey == validator_id.to_string())
            .and_then(|contact| contact.gossip)
            .and_then(|addr| match addr {
                SocketAddr::V4(addr_v4) => Some(*addr_v4.ip()),
                SocketAddr::V6(addr_v6) => addr_v6.ip().to_ipv4_mapped(),
            });
        Ok(address)
    }
}

pub struct SolPubsubClient {
    client: PubsubClient,
}

impl SolPubsubClient {
    pub async fn new(ws_url: Url) -> Result<Self> {
        let client = PubsubClient::new(ws_url.as_ref()).await?;

        Ok(Self { client })
    }

    pub async fn subscribe_to_access_requests(
        &self,
    ) -> Result<(
        BoxStream<'_, Response<RpcLogsResponse>>,
        Box<dyn FnOnce() -> BoxFuture<'static, ()> + Send>,
    )> {
        let config = RpcTransactionLogsConfig {
            commitment: Some(CommitmentConfig::confirmed()),
        };

        let filter = RpcTransactionLogsFilter::Mentions(vec![passport_id().to_string()]);

        Ok(self.client.logs_subscribe(filter, config).await?)
    }
}

/// Helper function to deserialize AccessMode from AccessRequest account data.
fn deserialize_access_request_from_account(
    request_pda: &Pubkey,
    account_data: &[u8],
) -> Result<AccessId> {
    // Parse the AccessRequest structure using zero_copy
    let (access_request, _) =
        zero_copy::checked_from_bytes_with_discriminator::<AccessRequest>(account_data)
            .ok_or_else(|| Error::Deserialize("Failed to deserialize AccessRequest".to_string()))?;

    // Deserialize safely
    let access_mode = access_request
        .checked_access_mode()
        .ok_or_else(|| Error::Deserialize("Failed to deserialize AccessMode".to_string()))?;

    Ok(AccessId {
        request_pda: *request_pda,
        rent_beneficiary_key: access_request.rent_beneficiary_key,
        mode: access_mode,
    })
}

fn is_request_access_instruction(ix: &CompiledInstruction, static_account_keys: &[Pubkey]) -> bool {
    ix.program_id(static_account_keys) == &passport_id()
        && Discriminator::new(ix.data[..8].try_into().unwrap())
            == PassportInstructionData::REQUEST_ACCESS
}

pub struct PreviousEpochSlots {
    current: u64,
    step: u64,
}

impl PreviousEpochSlots {
    // Number of slots per epoch
    const SLOTS_PER_EPOCH: u64 = 432_000;

    pub fn new(start: u64) -> Self {
        Self {
            current: start,
            step: Self::SLOTS_PER_EPOCH,
        }
    }
}

impl Iterator for PreviousEpochSlots {
    type Item = u64;

    fn next(&mut self) -> Option<Self::Item> {
        let result = self.current;
        if self.current < self.step {
            return None;
        }
        self.current -= self.step;
        Some(result)
    }
}

#[cfg(test)]
mod test {
    use super::*;

    #[test]
    fn test_reverse_iter() {
        let start_slot = 2_000_000;
        let num_epochs = 4;
        let epoch_slots = PreviousEpochSlots::new(start_slot)
            .take(num_epochs)
            .collect::<Vec<_>>();
        assert_eq!(epoch_slots.len(), 4);
        assert_eq!(epoch_slots.first().unwrap(), &start_slot);
        assert_eq!(
            epoch_slots.last().unwrap(),
            &(start_slot - 3 * PreviousEpochSlots::SLOTS_PER_EPOCH),
        );
    }
}
