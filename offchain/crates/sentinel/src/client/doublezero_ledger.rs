use std::{net::Ipv4Addr, sync::Arc, time::Duration};

use async_trait::async_trait;
use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_record::instruction as record_instruction;
use doublezero_sdk::get_tenant_pda;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_accesspass_pda, get_globalstate_pda},
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
use mockall::automock;
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::{
    nonblocking::rpc_client::RpcClient,
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_commitment_config::CommitmentConfig;
use solana_sdk::{
    instruction::AccountMeta,
    pubkey::Pubkey,
    signature::{Keypair, Signature, Signer},
};
use solana_system_interface::{instruction as system_instruction, program as system_program};
use tracing::info;
use url::Url;

use crate::{
    BILLING_RECEIPT_SEED_PREFIX, BillingReceipt, Error, Result, TenantBillingInfo, new_transaction,
};

/// Timeout for `send_and_confirm_transaction` calls to prevent the polling
/// loop from stalling on slow RPC confirmations.
const SEND_AND_CONFIRM_TIMEOUT: Duration = Duration::from_secs(60);
const SOLANA_TENANT_NAME: &str = "solana";

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
        let (globalstate_pk, _) = get_globalstate_pda(&self.serviceability_id);
        let (pass_pk, _) = get_accesspass_pda(&self.serviceability_id, client_ip, service_key);
        let (tenant_pk, _) = get_tenant_pda(&self.serviceability_id, SOLANA_TENANT_NAME);

        let args = DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::SolanaValidator(*validator_id),
            client_ip: *client_ip,
            last_access_epoch: u64::MAX,
            // NOTE: Setting this to false by default
            allow_multiple_ip: false,
            tenant: tenant_pk,
        });
        let accounts = vec![
            AccountMeta::new(pass_pk, false),
            AccountMeta::new_readonly(globalstate_pk, false),
            AccountMeta::new(*service_key, false),
            AccountMeta::new(self.payer.pubkey(), true),
            AccountMeta::new_readonly(system_program::id(), false),
        ];

        let set_pass_ix = try_build_instruction(&self.serviceability_id, accounts, &args)?;
        let signer = &self.payer;
        let recent_blockhash = self.client.get_latest_blockhash().await?;
        let transaction = new_transaction(&[set_pass_ix], &[signer], recent_blockhash);

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
        let (globalstate_pk, _) = get_globalstate_pda(&self.serviceability_id);
        let (accesspass_pk, _) =
            get_accesspass_pda(&self.serviceability_id, client_ip, service_key);

        let args = DoubleZeroInstruction::AddMulticastGroupPubAllowlist(
            AddMulticastGroupPubAllowlistArgs {
                client_ip: *client_ip,
                user_payer: *service_key,
            },
        );

        let accounts = vec![
            AccountMeta::new(*multicast_group_pda, false),
            AccountMeta::new(accesspass_pk, false),
            AccountMeta::new_readonly(globalstate_pk, false),
            AccountMeta::new(self.payer.pubkey(), true),
            AccountMeta::new_readonly(system_program::id(), false),
        ];

        let ix = try_build_instruction(&self.serviceability_id, accounts, &args)?;
        let signer = &self.payer;
        let recent_blockhash = self.client.get_latest_blockhash().await?;
        let transaction = new_transaction(&[ix], &[signer], recent_blockhash);

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
        let (globalstate_pk, _) = get_globalstate_pda(&self.serviceability_id);
        let args = DoubleZeroInstruction::UpdatePaymentStatus(UpdatePaymentStatusArgs {
            payment_status: status as u8,
            last_deduction_dz_epoch: None,
        });
        let accounts = vec![
            AccountMeta::new(*tenant_pda, false),
            AccountMeta::new_readonly(globalstate_pk, false),
            AccountMeta::new(self.payer.pubkey(), true),
            AccountMeta::new_readonly(system_program::id(), false),
        ];

        let ix = try_build_instruction(&self.serviceability_id, accounts, &args)?;
        let signer = &self.payer;
        let recent_blockhash = self.client.get_latest_blockhash().await?;
        let transaction = new_transaction(&[ix], &[signer], recent_blockhash);

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
        let epoch_bytes = receipt.dz_epoch.to_le_bytes();
        let seeds: &[&[u8]] = &[
            BILLING_RECEIPT_SEED_PREFIX,
            tenant_pda.as_ref(),
            &epoch_bytes,
        ];
        let payer_key = self.payer.pubkey();
        let serialized = borsh::to_vec(receipt)?;

        // Record account creation instructions (allocate, assign, initialize)
        let init = doublezero_sdk::record::instruction::InitializeRecordInstructions::new(
            &payer_key,
            seeds,
            serialized.len(),
        );

        // Transfer rent lamports
        let record_key = doublezero_sdk::record::pubkey::create_record_key(&payer_key, seeds);
        let rent_lamports = self
            .client
            .get_minimum_balance_for_rent_exemption(init.total_space)
            .await?;
        let transfer_ix = system_instruction::transfer(&payer_key, &record_key, rent_lamports);

        // Write receipt data
        let write_ix = record_instruction::write(&record_key, &payer_key, 0, &serialized);

        // Update epoch (UpdatePaymentStatus with last_deduction_dz_epoch)
        let (globalstate_pk, _) = get_globalstate_pda(&self.serviceability_id);
        let args = DoubleZeroInstruction::UpdatePaymentStatus(UpdatePaymentStatusArgs {
            payment_status: TenantPaymentStatus::Paid as u8,
            last_deduction_dz_epoch: Some(receipt.dz_epoch),
        });
        let accounts = vec![
            AccountMeta::new(*tenant_pda, false),
            AccountMeta::new_readonly(globalstate_pk, false),
            AccountMeta::new(payer_key, true),
            AccountMeta::new_readonly(system_program::id(), false),
        ];
        let epoch_ix = try_build_instruction(&self.serviceability_id, accounts, &args)?;

        let signer = &self.payer;
        let recent_blockhash = self.client.get_latest_blockhash().await?;
        let transaction = new_transaction(
            &[
                init.allocate,
                init.assign,
                transfer_ix,
                init.initialize,
                write_ix,
                epoch_ix,
            ],
            &[signer],
            recent_blockhash,
        );

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
