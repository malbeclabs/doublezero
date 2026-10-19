use std::{net::Ipv4Addr, sync::Arc, time::Duration};

use async_trait::async_trait;
use doublezero_record::instruction as record_instruction;
use doublezero_serviceability::{
    pda::get_accesspass_pda,
    processors::{
        accesspass::set::SetAccessPassArgs,
        multicastgroup::allowlist::publisher::add::AddMulticastGroupPubAllowlistArgs,
        tenant::update_payment_status::UpdatePaymentStatusArgs,
    },
    state::{
        accesspass::{AccessPass, AccessPassType},
        accounttype::AccountType,
        tenant::{Tenant, TenantPaymentStatus},
    },
};
use doublezero_serviceability_instruction as serviceability_instruction;
use mockall::automock;
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::{
    nonblocking::rpc_client::RpcClient,
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{
    hash::Hash,
    instruction::Instruction,
    pubkey::Pubkey,
    signature::{Keypair, Signature, Signer},
    transaction::VersionedTransaction,
};
use solana_system_interface::instruction as system_instruction;
use tracing::info;
use url::Url;

use crate::{
    BILLING_RECEIPT_SEED_PREFIX, BillingReceipt, Error, Result, TenantBillingInfo, new_transaction,
};

/// Builds a DoubleZero Ledger transaction: the serviceability compute-budget prelude (protocol-max
/// compute-unit limit and heap frame) followed by `instructions`. The serviceability program
/// allocates from the heap frame the transaction requests, and runs on the default 32 KiB without
/// one.
fn new_serviceability_transaction(
    instructions: &[Instruction],
    signers: &[&Keypair],
    recent_blockhash: Hash,
) -> VersionedTransaction {
    let mut ixs = serviceability_instruction::compute_budget_prelude().to_vec();
    ixs.extend_from_slice(instructions);
    new_transaction(&ixs, signers, recent_blockhash)
}

fn billing_receipt_seeds<'a>(tenant_pda: &'a Pubkey, epoch_bytes: &'a [u8; 8]) -> [&'a [u8]; 3] {
    [
        BILLING_RECEIPT_SEED_PREFIX,
        tenant_pda.as_ref(),
        epoch_bytes,
    ]
}

/// Size of the record account that holds `receipt`.
fn billing_receipt_record_space(
    payer_key: &Pubkey,
    tenant_pda: &Pubkey,
    receipt: &BillingReceipt,
) -> Result<usize> {
    let epoch_bytes = receipt.dz_epoch.to_le_bytes();
    let seeds = billing_receipt_seeds(tenant_pda, &epoch_bytes);
    let serialized = borsh::to_vec(receipt)?;
    Ok(
        doublezero_sdk::record::instruction::InitializeRecordInstructions::new(
            payer_key,
            &seeds,
            serialized.len(),
        )
        .total_space,
    )
}

/// Creates the record account holding `receipt`, funded with `rent_lamports`, and records the
/// receipt's epoch on the tenant, all in one transaction.
fn billing_receipt_instructions(
    serviceability_id: &Pubkey,
    payer_key: &Pubkey,
    tenant_pda: &Pubkey,
    receipt: &BillingReceipt,
    rent_lamports: u64,
) -> Result<Vec<Instruction>> {
    let epoch_bytes = receipt.dz_epoch.to_le_bytes();
    let seeds = billing_receipt_seeds(tenant_pda, &epoch_bytes);
    let serialized = borsh::to_vec(receipt)?;

    // Record account creation instructions (allocate, assign, initialize)
    let init = doublezero_sdk::record::instruction::InitializeRecordInstructions::new(
        payer_key,
        &seeds,
        serialized.len(),
    );

    // Transfer rent lamports
    let record_key = doublezero_sdk::record::pubkey::create_record_key(payer_key, &seeds);
    let transfer_ix = system_instruction::transfer(payer_key, &record_key, rent_lamports);

    // Write receipt data
    let write_ix = record_instruction::write(&record_key, payer_key, 0, &serialized);

    // Update epoch (UpdatePaymentStatus with last_deduction_dz_epoch)
    let epoch_ix = serviceability_instruction::tenant::update_payment_status(
        serviceability_id,
        payer_key,
        tenant_pda,
        UpdatePaymentStatusArgs {
            payment_status: TenantPaymentStatus::Paid as u8,
            last_deduction_dz_epoch: Some(receipt.dz_epoch),
        },
    );

    Ok(vec![
        init.allocate,
        init.assign,
        transfer_ix,
        init.initialize,
        write_ix,
        epoch_ix,
    ])
}

/// Timeout for `send_and_confirm_transaction` calls to prevent the polling
/// loop from stalling on slow RPC confirmations.
const SEND_AND_CONFIRM_TIMEOUT: Duration = Duration::from_secs(60);

#[automock]
#[async_trait]
pub trait DzRpcClientType {
    async fn issue_access_pass(
        &self,
        service_key: &Pubkey,
        client_ip: &Ipv4Addr,
        validator_id: &Pubkey,
    ) -> Result<Signature>;

    async fn add_multicast_publisher_allowlist(
        &self,
        multicast_group_pda: &Pubkey,
        service_key: &Pubkey,
        client_ip: &Ipv4Addr,
    ) -> Result<Signature>;

    async fn get_access_pass(
        &self,
        service_key: &Pubkey,
        client_ip: &Ipv4Addr,
    ) -> Result<AccessPass>;

    async fn get_tenants_with_token_accounts(&self) -> Result<Vec<TenantBillingInfo>>;

    async fn update_tenant_payment_status(
        &self,
        tenant_pda: &Pubkey,
        status: TenantPaymentStatus,
    ) -> Result<Signature>;

    async fn billing_receipt_exists(&self, tenant_pda: &Pubkey, epoch: u64) -> Result<bool>;

    async fn create_billing_receipt_and_update_epoch(
        &self,
        tenant_pda: &Pubkey,
        receipt: &BillingReceipt,
    ) -> Result<Signature>;

    async fn get_current_dz_epoch(&self) -> Result<u64>;
}

pub struct DzRpcClient {
    client: RpcClient,
    payer: Arc<Keypair>,
    serviceability_id: Pubkey,
}

#[async_trait]
impl DzRpcClientType for DzRpcClient {
    async fn issue_access_pass(
        &self,
        service_key: &Pubkey,
        client_ip: &Ipv4Addr,
        validator_id: &Pubkey,
    ) -> Result<Signature> {
        self.issue_access_pass(service_key, client_ip, validator_id)
            .await
    }

    async fn add_multicast_publisher_allowlist(
        &self,
        multicast_group_pda: &Pubkey,
        service_key: &Pubkey,
        client_ip: &Ipv4Addr,
    ) -> Result<Signature> {
        self.add_multicast_publisher_allowlist(multicast_group_pda, service_key, client_ip)
            .await
    }

    async fn get_access_pass(
        &self,
        service_key: &Pubkey,
        client_ip: &Ipv4Addr,
    ) -> Result<AccessPass> {
        self.get_access_pass(service_key, client_ip).await
    }

    async fn get_tenants_with_token_accounts(&self) -> Result<Vec<TenantBillingInfo>> {
        self.get_tenants_with_token_accounts().await
    }

    async fn update_tenant_payment_status(
        &self,
        tenant_pda: &Pubkey,
        status: TenantPaymentStatus,
    ) -> Result<Signature> {
        self.update_tenant_payment_status(tenant_pda, status).await
    }

    async fn billing_receipt_exists(&self, tenant_pda: &Pubkey, epoch: u64) -> Result<bool> {
        self.billing_receipt_exists(tenant_pda, epoch).await
    }

    async fn create_billing_receipt_and_update_epoch(
        &self,
        tenant_pda: &Pubkey,
        receipt: &BillingReceipt,
    ) -> Result<Signature> {
        self.create_billing_receipt_and_update_epoch(tenant_pda, receipt)
            .await
    }

    async fn get_current_dz_epoch(&self) -> Result<u64> {
        self.get_current_dz_epoch().await
    }
}

impl DzRpcClient {
    pub fn new(rpc_url: Url, payer: Arc<Keypair>, serviceability_id: Pubkey) -> Self {
        Self {
            client: RpcClient::new_with_commitment(
                rpc_url.clone().into(),
                CommitmentConfig::confirmed(),
            ),
            payer,
            serviceability_id,
        }
    }

    pub async fn issue_access_pass(
        &self,
        service_key: &Pubkey,
        client_ip: &Ipv4Addr,
        validator_id: &Pubkey,
    ) -> Result<Signature> {
        let set_pass_ix = serviceability_instruction::accesspass::set_access_pass(
            &self.serviceability_id,
            &self.payer.pubkey(),
            service_key,
            // No tenant change: default keys make the builder omit both tenant slots.
            &Pubkey::default(),
            &Pubkey::default(),
            SetAccessPassArgs {
                accesspass_type: AccessPassType::SolanaValidator(*validator_id),
                client_ip: *client_ip,
                last_access_epoch: u64::MAX,
                // NOTE: Setting this to false by default
                allow_multiple_ip: false,
                // NOTE: Setting both to max allowed values
                max_unicast_users: u16::MAX,
                max_multicast_users: u16::MAX,
            },
        );
        let signer = &self.payer;
        let recent_blockhash = self.client.get_latest_blockhash().await?;
        let transaction =
            new_serviceability_transaction(&[set_pass_ix], &[signer], recent_blockhash);

        let signature = self
            .client
            .send_and_confirm_transaction(&transaction)
            .await?;
        info!(validator = %service_key, %signature, "issued validator access pass");

        Ok(signature)
    }

    pub async fn add_multicast_publisher_allowlist(
        &self,
        multicast_group_pda: &Pubkey,
        service_key: &Pubkey,
        client_ip: &Ipv4Addr,
    ) -> Result<Signature> {
        let (accesspass_key, _) =
            get_accesspass_pda(&self.serviceability_id, client_ip, service_key);
        let ix = serviceability_instruction::multicastgroup::add_multicast_group_pub_allowlist(
            &self.serviceability_id,
            &self.payer.pubkey(),
            multicast_group_pda,
            &accesspass_key,
            AddMulticastGroupPubAllowlistArgs {
                client_ip: *client_ip,
                user_payer: *service_key,
            },
        );
        let signer = &self.payer;
        let recent_blockhash = self.client.get_latest_blockhash().await?;
        let transaction = new_serviceability_transaction(&[ix], &[signer], recent_blockhash);

        let signature = tokio::time::timeout(
            SEND_AND_CONFIRM_TIMEOUT,
            self.client.send_and_confirm_transaction(&transaction),
        )
        .await
        .map_err(|_| Error::Deserialize("add_multicast_publisher_allowlist timed out".into()))??;
        info!(
            validator = %service_key,
            %multicast_group_pda,
            %client_ip,
            %signature,
            "added to multicast publisher allowlist"
        );

        Ok(signature)
    }

    pub async fn get_access_pass(
        &self,
        service_key: &Pubkey,
        client_ip: &Ipv4Addr,
    ) -> Result<AccessPass> {
        let (accesspass_pk, _) =
            get_accesspass_pda(&self.serviceability_id, client_ip, service_key);
        let account_data = self.client.get_account_data(&accesspass_pk).await?;
        let access_pass = AccessPass::try_from(&account_data[..])
            .map_err(|e| Error::Deserialize(format!("AccessPass deserialization failed: {e}")))?;
        Ok(access_pass)
    }

    pub async fn get_tenants_with_token_accounts(&self) -> Result<Vec<TenantBillingInfo>> {
        let config = RpcProgramAccountsConfig {
            filters: Some(vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                0,
                vec![AccountType::Tenant as u8],
            ))]),
            account_config: RpcAccountInfoConfig {
                encoding: Some(UiAccountEncoding::Base64),
                ..Default::default()
            },
            ..Default::default()
        };

        let accounts = self
            .client
            .get_program_accounts_with_config(&self.serviceability_id, config)
            .await?;

        let tenants = accounts
            .into_iter()
            .filter_map(|(pubkey, account)| {
                let tenant = Tenant::try_from(&account.data[..]).ok()?;
                if tenant.token_account == Pubkey::default() {
                    return None;
                }
                Some(TenantBillingInfo {
                    tenant_pda: pubkey,
                    token_account: tenant.token_account,
                    current_payment_status: tenant.payment_status,
                    billing: tenant.billing,
                })
            })
            .collect();

        Ok(tenants)
    }

    pub async fn update_tenant_payment_status(
        &self,
        tenant_pda: &Pubkey,
        status: TenantPaymentStatus,
    ) -> Result<Signature> {
        let ix = serviceability_instruction::tenant::update_payment_status(
            &self.serviceability_id,
            &self.payer.pubkey(),
            tenant_pda,
            UpdatePaymentStatusArgs {
                payment_status: status as u8,
                last_deduction_dz_epoch: None,
            },
        );
        let signer = &self.payer;
        let recent_blockhash = self.client.get_latest_blockhash().await?;
        let transaction = new_serviceability_transaction(&[ix], &[signer], recent_blockhash);

        let signature = tokio::time::timeout(
            SEND_AND_CONFIRM_TIMEOUT,
            self.client.send_and_confirm_transaction(&transaction),
        )
        .await
        .map_err(|_| Error::Deserialize("update_payment_status timed out".into()))??;
        info!(tenant = %tenant_pda, ?status, %signature, "updated tenant payment status");

        Ok(signature)
    }

    pub async fn billing_receipt_exists(&self, tenant_pda: &Pubkey, epoch: u64) -> Result<bool> {
        let seeds: &[&[u8]] = &[
            BILLING_RECEIPT_SEED_PREFIX,
            tenant_pda.as_ref(),
            &epoch.to_le_bytes(),
        ];
        let record_key =
            doublezero_sdk::record::pubkey::create_record_key(&self.payer.pubkey(), seeds);
        let account = self
            .client
            .get_account_with_commitment(&record_key, CommitmentConfig::confirmed())
            .await?;
        Ok(account.value.is_some())
    }

    pub async fn create_billing_receipt_and_update_epoch(
        &self,
        tenant_pda: &Pubkey,
        receipt: &BillingReceipt,
    ) -> Result<Signature> {
        let space = billing_receipt_record_space(&self.payer.pubkey(), tenant_pda, receipt)?;
        let rent_lamports = self
            .client
            .get_minimum_balance_for_rent_exemption(space)
            .await?;
        let instructions = billing_receipt_instructions(
            &self.serviceability_id,
            &self.payer.pubkey(),
            tenant_pda,
            receipt,
            rent_lamports,
        )?;

        let signer = &self.payer;
        let recent_blockhash = self.client.get_latest_blockhash().await?;
        let transaction =
            new_serviceability_transaction(&instructions, &[signer], recent_blockhash);

        let signature = tokio::time::timeout(
            SEND_AND_CONFIRM_TIMEOUT,
            self.client.send_and_confirm_transaction(&transaction),
        )
        .await
        .map_err(|_| Error::Deserialize("create_billing_receipt timed out".into()))??;
        info!(
            tenant = %tenant_pda,
            epoch = receipt.dz_epoch,
            %signature,
            "created billing receipt and updated epoch"
        );

        Ok(signature)
    }

    /// Returns the last completed DZ epoch.
    ///
    /// Queries `getEpochInfo` on the **DZ Ledger RPC** (`self.client`), which
    /// runs its own epoch schedule independent of Solana mainnet/testnet. The
    /// billable epoch is `epoch - 1` because the current DZ Ledger epoch is
    /// still in progress. This matches how revenue-distribution syncs via
    /// `ProgramConfig.next_completed_dz_epoch` (see
    /// validator-debt/worker/initialize_distribution.rs).
    pub async fn get_current_dz_epoch(&self) -> Result<u64> {
        let epoch_info = self.client.get_epoch_info().await?;
        Ok(epoch_info.epoch.saturating_sub(1))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Largest serialized transaction the cluster accepts (`PACKET_DATA_SIZE`).
    const PACKET_DATA_SIZE: usize = 1232;

    fn receipt(tenant: Pubkey) -> BillingReceipt {
        BillingReceipt {
            tenant,
            dz_epoch: u64::MAX,
            amount: u64::MAX,
            sol_tx_sig: [0xff; 64],
        }
    }

    #[test]
    fn serviceability_transactions_start_with_the_compute_budget_prelude() {
        let payer = Keypair::new();
        let ix = serviceability_instruction::tenant::update_payment_status(
            &Pubkey::new_unique(),
            &payer.pubkey(),
            &Pubkey::new_unique(),
            UpdatePaymentStatusArgs {
                payment_status: TenantPaymentStatus::Paid as u8,
                last_deduction_dz_epoch: None,
            },
        );
        let tx = new_serviceability_transaction(
            std::slice::from_ref(&ix),
            &[&payer],
            Hash::new_unique(),
        );

        let message = &tx.message;
        let instructions = message.instructions();
        let prelude = serviceability_instruction::compute_budget_prelude();
        assert_eq!(instructions.len(), prelude.len() + 1);
        for (compiled, expected) in instructions.iter().zip(prelude.iter()) {
            assert_eq!(
                message.static_account_keys()[compiled.program_id_index as usize],
                expected.program_id
            );
            assert_eq!(compiled.data, expected.data);
        }
        assert_eq!(instructions[prelude.len()].data, ix.data);
    }

    /// The billing receipt is the largest transaction this client sends; it must still fit in a
    /// packet with the prelude in front.
    #[test]
    fn billing_receipt_transaction_with_prelude_fits_in_a_packet() {
        let payer = Keypair::new();
        let tenant = Pubkey::new_unique();
        let instructions = billing_receipt_instructions(
            &Pubkey::new_unique(),
            &payer.pubkey(),
            &tenant,
            &receipt(tenant),
            u64::MAX,
        )
        .unwrap();
        let tx = new_serviceability_transaction(&instructions, &[&payer], Hash::new_unique());

        let size = bincode::serialized_size(&tx).unwrap() as usize;
        assert!(size <= PACKET_DATA_SIZE, "{size} > {PACKET_DATA_SIZE}");
    }
}
