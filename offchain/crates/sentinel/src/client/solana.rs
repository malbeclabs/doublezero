use std::{
    net::{Ipv4Addr, SocketAddr},
    str::FromStr,
    sync::Arc,
    time::Duration,
};

use async_trait::async_trait;
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
use mockall::automock;
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::{
    nonblocking::rpc_client::RpcClient,
    rpc_config::{
        RpcAccountInfoConfig, RpcLeaderScheduleConfig, RpcProgramAccountsConfig,
        RpcTransactionConfig,
    },
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_commitment_config::{CommitmentConfig, CommitmentLevel};
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_sdk::{
    program_pack::Pack,
    pubkey::Pubkey,
    signature::{Keypair, Signature},
    signer::Signer,
};
use solana_transaction_status_client_types::{
    EncodedTransaction, UiInstruction, UiMessage, UiParsedInstruction, UiTransactionEncoding,
};
use url::Url;

use crate::{AccessId, Error, Result, new_transaction};

const ACCESS_REQUEST_ACCOUNT_INDEX: usize = 2;
/// Timeout for `send_and_confirm_transaction` calls to prevent the billing
/// polling loop from stalling on slow RPC confirmations.
const SEND_AND_CONFIRM_TIMEOUT: Duration = Duration::from_secs(60);

const SLOTS_PER_EPOCH: u64 = 432_000;

#[automock]
#[async_trait]
pub trait SolRpcClientType {
    async fn grant_access(
        &self,
        access_request_key: &Pubkey,
        rent_beneficiary_key: &Pubkey,
    ) -> Result<Signature>;

    async fn deny_access(&self, access_request_key: &Pubkey) -> Result<Signature>;

    async fn get_access_requests_from_signature(
        &self,
        signature: Signature,
    ) -> Result<Vec<AccessId>>;

    async fn get_access_requests(&self) -> Result<Vec<AccessId>>;

    async fn is_scheduled_leader(
        &self,
        validator_id: &Pubkey,
        previous_leader_epochs: u8,
    ) -> Result<bool>;

    async fn get_validator_ip(&self, validator_id: &Pubkey) -> Result<Option<Ipv4Addr>>;

    async fn get_token_account_balance(&self, token_account: &Pubkey) -> Result<u64>;

    async fn transfer_spl_token(
        &self,
        from: &Pubkey,
        to: &Pubkey,
        amount: u64,
    ) -> Result<Signature>;
}

pub struct SolRpcClient {
    client: RpcClient,
    payer: Arc<Keypair>,
}

#[async_trait]
impl SolRpcClientType for SolRpcClient {
    async fn grant_access(
        &self,
        access_request_key: &Pubkey,
        rent_beneficiary_key: &Pubkey,
    ) -> Result<Signature> {
        self.grant_access(access_request_key, rent_beneficiary_key)
            .await
    }

    async fn deny_access(&self, access_request_key: &Pubkey) -> Result<Signature> {
        self.deny_access(access_request_key).await
    }

    async fn get_access_requests_from_signature(
        &self,
        signature: Signature,
    ) -> Result<Vec<AccessId>> {
        self.get_access_requests_from_signature(signature).await
    }

    async fn get_access_requests(&self) -> Result<Vec<AccessId>> {
        self.get_access_requests().await
    }

    async fn is_scheduled_leader(
        &self,
        validator_id: &Pubkey,
        previous_leader_epochs: u8,
    ) -> Result<bool> {
        self.is_scheduled_leader(validator_id, previous_leader_epochs)
            .await
    }

    async fn get_validator_ip(&self, validator_id: &Pubkey) -> Result<Option<Ipv4Addr>> {
        self.get_validator_ip(validator_id).await
    }

    async fn get_token_account_balance(&self, token_account: &Pubkey) -> Result<u64> {
        self.get_token_account_balance(token_account).await
    }

    async fn transfer_spl_token(
        &self,
        from: &Pubkey,
        to: &Pubkey,
        amount: u64,
    ) -> Result<Signature> {
        self.transfer_spl_token(from, to, amount).await
    }
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
        let txn = self
            .client
            .get_transaction_with_config(
                &signature,
                RpcTransactionConfig {
                    encoding: Some(UiTransactionEncoding::JsonParsed),
                    commitment: Some(CommitmentConfig {
                        commitment: CommitmentLevel::Confirmed,
                    }),
                    max_supported_transaction_version: Some(1),
                },
            )
            .await?;

        let mut access_ids = Vec::new();
        for request_pda in access_request_pdas(&txn.transaction.transaction, signature)? {
            let account = self.client.get_account(&request_pda).await?;
            access_ids.push(deserialize_access_request_from_account(
                &request_pda,
                &account.data,
            )?);
        }

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

    /// NOTE: If previous_leader_epochs is 0, this method has no leader
    /// schedules to evaluate, so it will return false.
    pub async fn is_scheduled_leader(
        &self,
        validator_id: &Pubkey,
        previous_leader_epochs: u8,
    ) -> Result<bool> {
        if previous_leader_epochs == 0 {
            return Ok(false);
        }

        let epoch_slots = self.client.get_slot().await.map(PreviousEpochSlots)?;

        // We want to ensure that the number of leader schedules evaluated is
        // equal to the number of epochs requested.
        let mut schedule_count = 0;

        for slot in epoch_slots.take(previous_leader_epochs as usize) {
            let config = RpcLeaderScheduleConfig {
                identity: Some(validator_id.to_string()),
                ..Default::default()
            };

            let schedule = self
                .client
                .get_leader_schedule_with_config(Some(slot), config)
                .await?;

            // Bail out early if there is either no leader schedule or this
            // validator has no slot indices.
            if schedule.is_none_or(|slot_indices| slot_indices.is_empty()) {
                return Ok(false);
            }

            schedule_count += 1;
        }

        Ok(schedule_count == previous_leader_epochs)
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

    pub async fn get_token_account_balance(&self, token_account: &Pubkey) -> Result<u64> {
        let account = self.client.get_account(token_account).await?;
        let token_data = spl_token_interface::state::Account::unpack(&account.data)
            .map_err(|e| Error::Deserialize(format!("failed to unpack token account: {e}")))?;
        if token_data.owner != self.payer.pubkey() {
            return Err(Error::TokenAccountOwnerMismatch {
                account: *token_account,
                owner: token_data.owner,
                sentinel: self.payer.pubkey(),
            });
        }
        Ok(token_data.amount)
    }

    pub async fn transfer_spl_token(
        &self,
        from: &Pubkey,
        to: &Pubkey,
        amount: u64,
    ) -> Result<Signature> {
        let signer = &self.payer;
        let ix = spl_token_interface::instruction::transfer(
            &spl_token_interface::id(),
            from,
            to,
            &signer.pubkey(),
            &[],
            amount,
        )
        .map_err(|e| Error::SplInstruction(format!("transfer: {e}")))?;

        let recent_blockhash = self.client.get_latest_blockhash().await?;
        let transaction = new_transaction(&[ix], &[signer], recent_blockhash);

        let signature = tokio::time::timeout(
            SEND_AND_CONFIRM_TIMEOUT,
            self.client.send_and_confirm_transaction(&transaction),
        )
        .await
        .map_err(|_| Error::Deserialize("transfer_spl_token timed out".into()))??;

        Ok(signature)
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

fn access_request_pdas(
    transaction: &EncodedTransaction,
    signature: Signature,
) -> Result<Vec<Pubkey>> {
    let EncodedTransaction::Json(ui_transaction) = transaction else {
        return Err(Error::TransactionEncoding(signature));
    };
    let UiMessage::Parsed(message) = &ui_transaction.message else {
        return Err(Error::TransactionEncoding(signature));
    };

    let passport = passport_id().to_string();
    let mut pdas = Vec::new();
    for instruction in &message.instructions {
        let UiInstruction::Parsed(UiParsedInstruction::PartiallyDecoded(instruction)) = instruction
        else {
            continue;
        };
        if instruction.program_id != passport {
            continue;
        }
        let Ok(data) = bs58::decode(&instruction.data).into_vec() else {
            continue;
        };
        let Some(bytes) = data.get(..8) else {
            continue;
        };
        let Ok(bytes) = <[u8; 8]>::try_from(bytes) else {
            continue;
        };
        if Discriminator::new(bytes) != PassportInstructionData::REQUEST_ACCESS {
            continue;
        }

        let request_pda = instruction
            .accounts
            .get(ACCESS_REQUEST_ACCOUNT_INDEX)
            .ok_or(Error::InstructionInvalid(signature))?;
        let request_pda =
            Pubkey::from_str(request_pda).map_err(|_| Error::InstructionInvalid(signature))?;
        pdas.push(request_pda);
    }

    Ok(pdas)
}

struct PreviousEpochSlots(u64);

impl Iterator for PreviousEpochSlots {
    type Item = u64;

    fn next(&mut self) -> Option<Self::Item> {
        let next_slot = &mut self.0;

        if *next_slot == 0 {
            None
        } else {
            let current_slot = *next_slot;
            *next_slot = next_slot.saturating_sub(SLOTS_PER_EPOCH);
            Some(current_slot)
        }
    }
}

#[cfg(test)]
mod test {
    use borsh::BorshSerialize;
    use serde_json::json;
    use solana_transaction_status_client_types::EncodedTransactionWithStatusMeta;

    use super::*;

    fn encoded_ix_data(discriminator: Discriminator<8>) -> String {
        let mut data = Vec::new();
        discriminator.serialize(&mut data).unwrap();
        bs58::encode(data).into_string()
    }

    fn request_access_instruction(accounts: Vec<String>) -> serde_json::Value {
        json!({
            "programId": passport_id().to_string(),
            "accounts": accounts,
            "data": encoded_ix_data(PassportInstructionData::REQUEST_ACCESS),
            "stackHeight": null,
        })
    }

    fn parsed_transaction(
        instructions: Vec<serde_json::Value>,
        version: u8,
    ) -> EncodedTransaction {
        let encoded: EncodedTransactionWithStatusMeta = serde_json::from_value(json!({
            "transaction": {
                "signatures": [Signature::default().to_string()],
                "message": {
                    "accountKeys": [],
                    "recentBlockhash": "11111111111111111111111111111111",
                    "instructions": instructions,
                    "addressTableLookups": null,
                    "transactionConfig": {
                        "computeUnitLimit": 30_000,
                        "heapSize": null,
                        "loadedAccountsDataSizeLimit": 200_000,
                        "priorityFee": null,
                    },
                },
            },
            "meta": null,
            "version": version,
        }))
        .expect("a getTransaction JsonParsed response must deserialize");
        encoded.transaction
    }

    #[test]
    fn reads_a_request_access_pda_from_a_v1_json_parsed_transaction() {
        let request_pda = Pubkey::new_unique();
        let transaction = parsed_transaction(
            vec![request_access_instruction(vec![
                Pubkey::new_unique().to_string(),
                Pubkey::new_unique().to_string(),
                request_pda.to_string(),
                Pubkey::new_unique().to_string(),
            ])],
            1,
        );

        assert_eq!(
            access_request_pdas(&transaction, Signature::default()).unwrap(),
            vec![request_pda]
        );
    }

    #[test]
    fn ignores_instructions_that_are_not_request_access() {
        let transaction = parsed_transaction(
            vec![
                json!({
                    "programId": Pubkey::new_unique().to_string(),
                    "accounts": [],
                    "data": bs58::encode([1, 2, 3]).into_string(),
                    "stackHeight": null,
                }),
                json!({
                    "programId": passport_id().to_string(),
                    "accounts": [],
                    "data": encoded_ix_data(PassportInstructionData::GRANT_ACCESS),
                    "stackHeight": null,
                }),
            ],
            1,
        );

        assert_eq!(
            access_request_pdas(&transaction, Signature::default()).unwrap(),
            Vec::<Pubkey>::new()
        );
    }

    #[test]
    fn test_reverse_iter() {
        let start_slot = 2_000_000;
        let num_epochs = 4;
        let epoch_slots = PreviousEpochSlots(start_slot)
            .take(num_epochs)
            .collect::<Vec<_>>();
        assert_eq!(epoch_slots.len(), 4);
        assert_eq!(epoch_slots.first().unwrap(), &start_slot);
        assert_eq!(
            epoch_slots.last().unwrap(),
            &(start_slot - 3 * SLOTS_PER_EPOCH),
        );
    }
}
