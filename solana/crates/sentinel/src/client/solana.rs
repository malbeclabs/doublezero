use crate::{new_transaction, AccessIds, Error, Result};

use base64::{engine::general_purpose::STANDARD as BASE64_STD, Engine};
use bincode;
use borsh::de::BorshDeserialize;
use doublezero_passport::{
    id as passport_id,
    instruction::{
        account::{DenyAccessAccounts, GrantAccessAccounts},
        PassportInstructionData,
    },
    state::AccessRequest,
};
use doublezero_program_tools::{instruction::try_build_instruction, PrecomputedDiscriminator};
use futures::{future::BoxFuture, stream::BoxStream, StreamExt, TryStreamExt};
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::{
    nonblocking::{pubsub_client::PubsubClient, rpc_client::RpcClient},
    rpc_config::{
        RpcAccountInfoConfig, RpcLeaderScheduleConfig, RpcProgramAccountsConfig,
        RpcTransactionLogsConfig, RpcTransactionLogsFilter,
    },
    rpc_filter::{Memcmp, RpcFilterType},
    rpc_response::{Response, RpcLogsResponse},
};
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{
    pubkey::Pubkey,
    signature::{Keypair, Signature},
    signer::Signer,
    transaction::Transaction,
};
use solana_transaction_status_client_types::{
    EncodedTransaction, TransactionBinaryEncoding, UiTransactionEncoding,
};
use std::sync::Arc;
use url::Url;

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

        let transaction = new_transaction(&[grant_ix], &[signer], recent_blockhash);

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

        let recent_blockhash = self.client.get_latest_blockhash().await?;

        let transaction = new_transaction(&[deny_ix], &[signer], recent_blockhash);

        Ok(self
            .client
            .send_and_confirm_transaction(&transaction)
            .await?)
    }

    pub async fn get_access_request_from_signature(
        &self,
        signature: Signature,
    ) -> Result<AccessIds> {
        let txn = self
            .client
            .get_transaction(&signature, UiTransactionEncoding::Binary)
            .await?;

        if let EncodedTransaction::Binary(data, TransactionBinaryEncoding::Base64) =
            txn.transaction.transaction
        {
            let data: &[u8] = &BASE64_STD.decode(data)?;
            let tx: Transaction = bincode::deserialize(data)?;

            deserialize_access_request_ids(tx)
        } else {
            Err(Error::TransactionEncoding(signature))
        }
    }

    pub async fn get_access_requests(&self) -> Result<Vec<AccessIds>> {
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

        let access_ids = futures::stream::iter(accounts)
            .then(|(pubkey, _acct)| async move {
                let signatures = self.client.get_signatures_for_address(&pubkey).await?;

                let creation_signature: Signature = signatures
                    .first()
                    .ok_or(Error::MissingTxnSignature)
                    .and_then(|sig| sig.signature.parse().map_err(Error::from))?;

                self.get_access_request_from_signature(creation_signature)
                    .await
            })
            .try_collect::<Vec<_>>()
            .await?;

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

fn deserialize_access_request_ids(txn: Transaction) -> Result<AccessIds> {
    let signature = txn.signatures.first().ok_or(Error::MissingTxnSignature)?;
    let compiled_ix = txn
        .message
        .instructions
        .iter()
        .find(|ix| ix.program_id(&txn.message.account_keys) == &passport_id())
        .ok_or(Error::InstructionNotFound(*signature))?;
    let accounts = compiled_ix
        .accounts
        .iter()
        .map(|&idx| txn.message.account_keys.get(idx as usize).copied())
        .collect::<Option<Vec<_>>>()
        .ok_or(Error::MissingAccountKeys(*signature))?;
    let Ok(PassportInstructionData::RequestAccess(mode)) =
        PassportInstructionData::try_from_slice(&compiled_ix.data)
    else {
        return Err(Error::InstructionInvalid(*signature));
    };
    match (accounts.get(2), accounts.get(1)) {
        (Some(request_pda), Some(payer)) => Ok(AccessIds {
            request_pda: *request_pda,
            rent_beneficiary_key: *payer,
            mode,
        }),
        _ => Err(Error::InstructionInvalid(*signature)),
    }
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
