use std::{
    sync::Arc,
    time::{Duration, Instant},
};

use doublezero_serviceability::state::tenant::{TenantBillingConfig, TenantPaymentStatus};
use retainer::Cache;
use solana_sdk::pubkey::Pubkey;
use tokio::time::interval;
use tokio_util::sync::CancellationToken;
use tracing::{error, info, warn};

use crate::{
    BillingReceipt, TenantBillingInfo,
    client::{doublezero_ledger::DzRpcClientType, solana::SolRpcClientType},
    error::rpc_with_retry,
};

// cache ttl: 1 hour (covers realistic DZ outage windows; refreshed on each
// failed DZ tx attempt so effective TTL extends indefinitely during outages)
const BILLING_CACHE_TTL: Duration = Duration::from_secs(3600);
// cache monitoring interval, every 2 minutes
const BILLING_CACHE_MONITOR_INTERVAL: Duration = Duration::from_secs(120);

pub struct BillingConfig {
    pub poll_interval_secs: u64,
    pub minimum_balance: Option<u64>,
    pub journal_ata: Pubkey,
}

impl BillingConfig {
    pub fn new(poll_interval_secs: u64, minimum_balance: Option<u64>, journal_ata: Pubkey) -> Self {
        Self {
            poll_interval_secs,
            minimum_balance,
            journal_ata,
        }
    }
}

pub struct BillingSentinel<D: DzRpcClientType, S: SolRpcClientType> {
    dz_rpc_client: D,
    sol_rpc_client: S,
    status_cache: Arc<Cache<Pubkey, TenantPaymentStatus>>,
    /// In-process cache: tenant → (epoch, solana_signature_bytes).
    /// Prevents re-transferring when the DZ tx fails but the process is still running.
    transfer_cache: Arc<Cache<Pubkey, (u64, [u8; 64])>>,
    monitor_handles: Vec<tokio::task::JoinHandle<()>>,
    poll_interval: Duration,
    minimum_balance: u64,
    journal_ata: Pubkey,
}

impl<D: DzRpcClientType, S: SolRpcClientType> BillingSentinel<D, S> {
    pub async fn new(dz_rpc_client: D, sol_rpc_client: S, config: BillingConfig) -> Self {
        let status_cache = Arc::new(Cache::new());
        let transfer_cache = Arc::new(Cache::new());

        // Spawn background tasks to evict expired cache entries.
        // monitor(batch_size, sample_ratio, interval):
        //   batch_size=5  — check up to 5 entries per sweep
        //   sample_ratio=0.25 — sample 25% of the cache per sweep
        //   interval — time between sweeps
        let status_clone = status_cache.clone();
        let status_handle = tokio::spawn(async move {
            status_clone
                .monitor(5, 0.25, BILLING_CACHE_MONITOR_INTERVAL)
                .await;
        });
        let transfer_clone = transfer_cache.clone();
        let transfer_handle = tokio::spawn(async move {
            transfer_clone
                .monitor(5, 0.25, BILLING_CACHE_MONITOR_INTERVAL)
                .await;
        });

        Self {
            dz_rpc_client,
            sol_rpc_client,
            status_cache,
            transfer_cache,
            monitor_handles: vec![status_handle, transfer_handle],
            poll_interval: Duration::from_secs(config.poll_interval_secs),
            minimum_balance: config.minimum_balance.unwrap_or(1),
            journal_ata: config.journal_ata,
        }
    }

    pub async fn run(&mut self, shutdown_listener: CancellationToken) -> crate::Result<()> {
        let mut poll_timer = interval(self.poll_interval);

        loop {
            tokio::select! {
                biased;
                _ = shutdown_listener.cancelled() => {
                    info!("billing sentinel: shutdown signal received");
                    break;
                }
                _ = poll_timer.tick() => {
                    if let Err(err) = self.poll_cycle().await {
                        error!(?err, "billing poll cycle failed; will retry in next cycle");
                        metrics::counter!("doublezero_sentinel_billing_poll_failed").increment(1);
                    }
                }
            }
        }

        for handle in &self.monitor_handles {
            handle.abort();
        }

        Ok(())
    }

    async fn poll_cycle(&self) -> crate::Result<()> {
        let start = Instant::now();

        // Fetch current DZ epoch for deduction processing. If this fails,
        // we can still run legacy balance checks but skip deductions.
        let current_epoch = match rpc_with_retry(
            || self.dz_rpc_client.get_current_dz_epoch(),
            "billing_get_dz_epoch",
        )
        .await
        {
            Ok(epoch) => Some(epoch),
            Err(err) => {
                warn!(
                    ?err,
                    "billing: failed to fetch current DZ epoch; skipping deductions"
                );
                None
            }
        };

        let tenants = rpc_with_retry(
            || self.dz_rpc_client.get_tenants_with_token_accounts(),
            "billing_get_tenants",
        )
        .await?;

        let total = tenants.len();
        info!(count = total, "billing: checking tenant balances");
        metrics::gauge!("doublezero_sentinel_billing_tenants_checked").set(total as f64);

        let mut failures = 0u64;
        for tenant in tenants {
            // Currently only one variant exists, but guard against future
            // upstream additions so the sentinel degrades gracefully.
            #[allow(unreachable_patterns)]
            let config = match tenant.billing {
                TenantBillingConfig::FlatPerEpoch(ref c) => c,
                _ => {
                    warn!(
                        tenant = %tenant.tenant_pda,
                        "billing: unknown billing config variant; skipping"
                    );
                    continue;
                }
            };

            let result = if config.rate > 0 {
                if let Some(epoch) = current_epoch {
                    self.deduct_tenant(&tenant, epoch).await
                } else {
                    // Can't process deductions without knowing the current epoch
                    continue;
                }
            } else {
                // Legacy balance-check path (rate == 0)
                self.check_and_update_tenant(&tenant).await
            };

            if let Err(err) = result {
                failures += 1;
                warn!(
                    tenant = %tenant.tenant_pda,
                    ?err,
                    "billing: failed to process tenant"
                );
                metrics::counter!("doublezero_sentinel_billing_tenant_error").increment(1);
            }
        }

        if failures > 0 {
            warn!(
                failures,
                total, "billing: cycle completed with tenant failures"
            );
        }

        let elapsed = start.elapsed();
        metrics::histogram!("doublezero_sentinel_billing_cycle_duration_seconds")
            .record(elapsed.as_secs_f64());

        Ok(())
    }

    async fn deduct_tenant(
        &self,
        tenant: &TenantBillingInfo,
        current_epoch: u64,
    ) -> crate::Result<()> {
        // See poll_cycle — guard against future upstream variants.
        #[allow(unreachable_patterns)]
        let config = match tenant.billing {
            TenantBillingConfig::FlatPerEpoch(ref c) => c,
            _ => {
                warn!(
                    tenant = %tenant.tenant_pda,
                    "billing: unknown billing config variant; skipping deduction"
                );
                return Ok(());
            }
        };

        // Already current — nothing to deduct
        if config.last_deduction_dz_epoch >= current_epoch {
            return Ok(());
        }

        let target_epoch = config.last_deduction_dz_epoch + 1;

        // Check in-process transfer cache (fast path — avoids re-transferring
        // when the DZ tx failed but process is still running).
        // A cached entry for epoch N is valid for deducting epoch N because the
        // sentinel advances one epoch at a time.
        let cached = self.transfer_cache.get(&tenant.tenant_pda).await;
        let cached_hit = cached.as_ref().filter(|entry| entry.0 >= target_epoch);

        let sig_bytes = if let Some(entry) = cached_hit {
            entry.1
        } else {
            // Check on-chain receipt on DZ Ledger
            if rpc_with_retry(
                || {
                    self.dz_rpc_client
                        .billing_receipt_exists(&tenant.tenant_pda, target_epoch)
                },
                "billing_receipt_exists",
            )
            .await?
            {
                info!(
                    tenant = %tenant.tenant_pda,
                    target_epoch,
                    "billing: receipt already exists, skipping"
                );
                return Ok(());
            }

            info!(
                tenant = %tenant.tenant_pda,
                rate = config.rate,
                target_epoch,
                current_epoch,
                "billing: deducting tenant"
            );

            // Attempt the SPL token transfer on Solana
            match self
                .sol_rpc_client
                .transfer_spl_token(&tenant.token_account, &self.journal_ata, config.rate)
                .await
            {
                Ok(signature) => {
                    info!(
                        tenant = %tenant.tenant_pda,
                        %signature,
                        target_epoch,
                        "billing: transfer successful"
                    );
                    let bytes: [u8; 64] = signature.into();
                    self.transfer_cache
                        .insert(tenant.tenant_pda, (target_epoch, bytes), BILLING_CACHE_TTL)
                        .await;
                    bytes
                }
                Err(err) => {
                    warn!(
                        tenant = %tenant.tenant_pda,
                        ?err,
                        "billing: deduction transfer failed, checking balance"
                    );

                    // Check whether the failure is due to insufficient balance
                    let balance = rpc_with_retry(
                        || {
                            self.sol_rpc_client
                                .get_token_account_balance(&tenant.token_account)
                        },
                        "billing_get_balance",
                    )
                    .await?;

                    if balance < config.rate {
                        info!(
                            tenant = %tenant.tenant_pda,
                            balance,
                            rate = config.rate,
                            "billing: insufficient balance, marking delinquent"
                        );
                        rpc_with_retry(
                            || {
                                self.dz_rpc_client.update_tenant_payment_status(
                                    &tenant.tenant_pda,
                                    TenantPaymentStatus::Delinquent,
                                )
                            },
                            "billing_update_status",
                        )
                        .await?;
                        self.status_cache
                            .insert(
                                tenant.tenant_pda,
                                TenantPaymentStatus::Delinquent,
                                BILLING_CACHE_TTL,
                            )
                            .await;
                        metrics::counter!("doublezero_sentinel_billing_status_delinquent")
                            .increment(1);
                    }
                    // If balance >= rate, it was a transient error — will retry next cycle
                    return Ok(());
                }
            }
        };

        // Create billing receipt and update epoch in a single atomic DZ Ledger tx
        let receipt = BillingReceipt {
            tenant: tenant.tenant_pda,
            dz_epoch: target_epoch,
            amount: config.rate,
            sol_tx_sig: sig_bytes,
        };

        match self
            .dz_rpc_client
            .create_billing_receipt_and_update_epoch(&tenant.tenant_pda, &receipt)
            .await
        {
            Ok(signature) => {
                info!(
                    tenant = %tenant.tenant_pda,
                    %signature,
                    target_epoch,
                    "billing: receipt created and epoch updated"
                );
                self.transfer_cache.remove(&tenant.tenant_pda).await;
                self.status_cache
                    .insert(
                        tenant.tenant_pda,
                        TenantPaymentStatus::Paid,
                        BILLING_CACHE_TTL,
                    )
                    .await;
                metrics::counter!("doublezero_sentinel_billing_deduction_success").increment(1);
                Ok(())
            }
            Err(err) => {
                warn!(
                    tenant = %tenant.tenant_pda,
                    target_epoch,
                    ?err,
                    "billing: DZ tx failed; checking if receipt landed"
                );

                // Check if receipt actually landed (ambiguous tx success /
                // allocate-existing-account error)
                if let Ok(true) = self
                    .dz_rpc_client
                    .billing_receipt_exists(&tenant.tenant_pda, target_epoch)
                    .await
                {
                    info!(
                        tenant = %tenant.tenant_pda,
                        target_epoch,
                        "billing: receipt exists despite tx error; treating as success"
                    );
                    self.transfer_cache.remove(&tenant.tenant_pda).await;
                    self.status_cache
                        .insert(
                            tenant.tenant_pda,
                            TenantPaymentStatus::Paid,
                            BILLING_CACHE_TTL,
                        )
                        .await;
                    metrics::counter!("doublezero_sentinel_billing_deduction_success").increment(1);
                    return Ok(());
                }

                // Refresh transfer_cache TTL to survive prolonged DZ outages
                self.transfer_cache
                    .insert(
                        tenant.tenant_pda,
                        (target_epoch, sig_bytes),
                        BILLING_CACHE_TTL,
                    )
                    .await;
                Err(err)
            }
        }
    }

    /// Legacy balance-check path for tenants with rate == 0.
    async fn check_and_update_tenant(&self, tenant: &TenantBillingInfo) -> crate::Result<()> {
        let balance = rpc_with_retry(
            || {
                self.sol_rpc_client
                    .get_token_account_balance(&tenant.token_account)
            },
            "billing_get_balance",
        )
        .await?;

        let new_status = self.derive_status(balance);

        // Check cache — skip if status hasn't changed
        if let Some(cached_status) = self.status_cache.get(&tenant.tenant_pda).await
            && *cached_status == new_status
        {
            return Ok(());
        }

        let current_onchain = tenant.current_payment_status;
        if current_onchain == new_status {
            // Cache the current status so we don't re-check until TTL
            self.status_cache
                .insert(tenant.tenant_pda, new_status, BILLING_CACHE_TTL)
                .await;
            return Ok(());
        }

        info!(
            tenant = %tenant.tenant_pda,
            old_status = ?current_onchain,
            new_status = ?new_status,
            balance,
            "billing: updating tenant payment status"
        );

        rpc_with_retry(
            || {
                self.dz_rpc_client
                    .update_tenant_payment_status(&tenant.tenant_pda, new_status)
            },
            "billing_update_status",
        )
        .await?;

        // Update cache
        self.status_cache
            .insert(tenant.tenant_pda, new_status, BILLING_CACHE_TTL)
            .await;

        // Emit status metrics
        match new_status {
            TenantPaymentStatus::Paid => {
                metrics::counter!("doublezero_sentinel_billing_status_paid").increment(1);
            }
            TenantPaymentStatus::Delinquent => {
                metrics::counter!("doublezero_sentinel_billing_status_delinquent").increment(1);
            }
        }

        Ok(())
    }

    fn derive_status(&self, balance: u64) -> TenantPaymentStatus {
        if balance >= self.minimum_balance {
            TenantPaymentStatus::Paid
        } else {
            TenantPaymentStatus::Delinquent
        }
    }
}

#[cfg(test)]
mod tests {
    use doublezero_serviceability::state::tenant::FlatPerEpochConfig;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    use super::*;
    use crate::client::{doublezero_ledger::MockDzRpcClientType, solana::MockSolRpcClientType};

    const TEST_JOURNAL_ATA: Pubkey = Pubkey::new_from_array([88; 32]);

    fn make_tenant(pda_byte: u8, token_byte: u8, status: TenantPaymentStatus) -> TenantBillingInfo {
        make_tenant_with_billing(pda_byte, token_byte, status, TenantBillingConfig::default())
    }

    fn make_tenant_with_billing(
        pda_byte: u8,
        token_byte: u8,
        status: TenantPaymentStatus,
        billing: TenantBillingConfig,
    ) -> TenantBillingInfo {
        let mut pda_bytes = [0u8; 32];
        pda_bytes[0] = pda_byte;
        let mut token_bytes = [0u8; 32];
        token_bytes[0] = token_byte;
        TenantBillingInfo {
            tenant_pda: Pubkey::new_from_array(pda_bytes),
            token_account: Pubkey::new_from_array(token_bytes),
            current_payment_status: status,
            billing,
        }
    }

    fn billing_config(rate: u64, last_epoch: u64) -> TenantBillingConfig {
        TenantBillingConfig::FlatPerEpoch(FlatPerEpochConfig {
            rate,
            last_deduction_dz_epoch: last_epoch,
        })
    }

    async fn new_sentinel(
        dz: MockDzRpcClientType,
        sol: MockSolRpcClientType,
    ) -> BillingSentinel<MockDzRpcClientType, MockSolRpcClientType> {
        BillingSentinel::new(
            dz,
            sol,
            BillingConfig::new(60, Some(1000), TEST_JOURNAL_ATA),
        )
        .await
    }

    // ── Legacy balance-check tests (rate == 0) ──────────────────────────

    #[tokio::test]
    async fn test_derive_status_paid() {
        let dz = MockDzRpcClientType::new();
        let sol = MockSolRpcClientType::new();

        let sentinel = new_sentinel(dz, sol).await;
        assert_eq!(sentinel.derive_status(1000), TenantPaymentStatus::Paid);
        assert_eq!(sentinel.derive_status(9999), TenantPaymentStatus::Paid);
    }

    #[tokio::test]
    async fn test_derive_status_delinquent() {
        let dz = MockDzRpcClientType::new();
        let sol = MockSolRpcClientType::new();

        let sentinel = new_sentinel(dz, sol).await;
        assert_eq!(sentinel.derive_status(999), TenantPaymentStatus::Delinquent);
        assert_eq!(sentinel.derive_status(1), TenantPaymentStatus::Delinquent);
    }

    #[tokio::test]
    async fn test_cache_prevents_redundant_writes() {
        let tenant = make_tenant(1, 2, TenantPaymentStatus::Delinquent);
        let tenant_pda = tenant.tenant_pda;
        let token_account = tenant.token_account;

        let mut dz = MockDzRpcClientType::new();
        let mut sol = MockSolRpcClientType::new();

        dz.expect_get_tenants_with_token_accounts()
            .returning(move || Ok(vec![make_tenant(1, 2, TenantPaymentStatus::Delinquent)]));

        // Balance check happens every call (cache only prevents DZ writes)
        sol.expect_get_token_account_balance()
            .with(predicate::eq(token_account))
            .times(2)
            .returning(|_| Ok(5000));

        // DZ write should only happen once — second call is skipped by cache
        dz.expect_update_tenant_payment_status()
            .with(
                predicate::eq(tenant_pda),
                predicate::eq(TenantPaymentStatus::Paid),
            )
            .times(1)
            .returning(|_, _| Ok(Signature::new_unique()));

        let sentinel = new_sentinel(dz, sol).await;

        // First check — should trigger update
        sentinel.check_and_update_tenant(&tenant).await.unwrap();

        // Second check — cache hit, no DZ write (mockall would panic if update called again)
        sentinel.check_and_update_tenant(&tenant).await.unwrap();
    }

    #[tokio::test]
    async fn test_onchain_status_matches_skips_write() {
        // Tenant already has Paid status onchain; balance still high.
        // Should NOT write to DZ Ledger, but should populate cache.
        let tenant = make_tenant(1, 2, TenantPaymentStatus::Paid);
        let token_account = tenant.token_account;

        let dz = MockDzRpcClientType::new();
        let mut sol = MockSolRpcClientType::new();

        sol.expect_get_token_account_balance()
            .with(predicate::eq(token_account))
            .times(1)
            .returning(|_| Ok(5000));

        // No update expected — status already matches
        // (mockall will panic if update_tenant_payment_status is called)

        let sentinel = new_sentinel(dz, sol).await;
        sentinel.check_and_update_tenant(&tenant).await.unwrap();
    }

    #[tokio::test]
    async fn test_paid_to_delinquent_transition() {
        // Tenant is Paid onchain but balance has dropped below threshold
        let tenant = make_tenant(1, 2, TenantPaymentStatus::Paid);
        let tenant_pda = tenant.tenant_pda;
        let token_account = tenant.token_account;

        let mut dz = MockDzRpcClientType::new();
        let mut sol = MockSolRpcClientType::new();

        sol.expect_get_token_account_balance()
            .with(predicate::eq(token_account))
            .times(1)
            .returning(|_| Ok(0));

        dz.expect_update_tenant_payment_status()
            .with(
                predicate::eq(tenant_pda),
                predicate::eq(TenantPaymentStatus::Delinquent),
            )
            .times(1)
            .returning(|_, _| Ok(Signature::new_unique()));

        let sentinel = new_sentinel(dz, sol).await;
        sentinel.check_and_update_tenant(&tenant).await.unwrap();
    }

    #[tokio::test]
    async fn test_multiple_tenants_tracked_independently() {
        let tenant_a = make_tenant(1, 10, TenantPaymentStatus::Delinquent);
        let tenant_b = make_tenant(2, 20, TenantPaymentStatus::Delinquent);

        let mut dz = MockDzRpcClientType::new();
        let mut sol = MockSolRpcClientType::new();

        // Tenant A: high balance
        sol.expect_get_token_account_balance()
            .with(predicate::eq(tenant_a.token_account))
            .returning(|_| Ok(5000));

        // Tenant B: zero balance
        sol.expect_get_token_account_balance()
            .with(predicate::eq(tenant_b.token_account))
            .returning(|_| Ok(0));

        dz.expect_update_tenant_payment_status()
            .with(
                predicate::eq(tenant_a.tenant_pda),
                predicate::eq(TenantPaymentStatus::Paid),
            )
            .times(1)
            .returning(|_, _| Ok(Signature::new_unique()));

        // No update expected for tenant_b — already Delinquent and stays Delinquent
        // (mockall will panic if update_tenant_payment_status is called with tenant_b)

        let sentinel = new_sentinel(dz, sol).await;

        sentinel.check_and_update_tenant(&tenant_a).await.unwrap();
        sentinel.check_and_update_tenant(&tenant_b).await.unwrap();
    }

    // ── Deduction path tests (rate > 0) ─────────────────────────────────

    #[tokio::test]
    async fn test_deduction_happy_path() {
        // Tenant with rate > 0, epoch behind current → should deduct
        let tenant = make_tenant_with_billing(
            1,
            2,
            TenantPaymentStatus::Paid,
            billing_config(1_000_000, 5),
        );
        let tenant_pda = tenant.tenant_pda;
        let token_account = tenant.token_account;

        let mut dz = MockDzRpcClientType::new();
        let mut sol = MockSolRpcClientType::new();

        // No receipt exists yet
        dz.expect_billing_receipt_exists()
            .with(predicate::eq(tenant_pda), predicate::eq(6))
            .times(1)
            .returning(|_, _| Ok(false));

        // Transfer succeeds
        sol.expect_transfer_spl_token()
            .with(
                predicate::eq(token_account),
                predicate::eq(TEST_JOURNAL_ATA),
                predicate::eq(1_000_000),
            )
            .times(1)
            .returning(|_, _, _| Ok(Signature::new_unique()));

        // Atomic receipt creation + epoch update
        dz.expect_create_billing_receipt_and_update_epoch()
            .with(predicate::eq(tenant_pda), predicate::always())
            .times(1)
            .returning(|_, _| Ok(Signature::new_unique()));

        let sentinel = new_sentinel(dz, sol).await;
        sentinel.deduct_tenant(&tenant, 10).await.unwrap();
    }

    #[tokio::test]
    async fn test_deduction_insufficient_balance() {
        // Transfer fails, balance < rate → Delinquent
        let tenant = make_tenant_with_billing(
            1,
            2,
            TenantPaymentStatus::Paid,
            billing_config(1_000_000, 5),
        );
        let tenant_pda = tenant.tenant_pda;
        let token_account = tenant.token_account;

        let mut dz = MockDzRpcClientType::new();
        let mut sol = MockSolRpcClientType::new();

        // No receipt exists
        dz.expect_billing_receipt_exists()
            .returning(|_, _| Ok(false));

        // Transfer fails
        sol.expect_transfer_spl_token()
            .times(1)
            .returning(|_, _, _| Err(crate::Error::Deserialize("insufficient funds".into())));

        // Balance check reveals insufficient funds
        sol.expect_get_token_account_balance()
            .with(predicate::eq(token_account))
            .times(1)
            .returning(|_| Ok(500_000)); // less than rate

        // Should mark Delinquent
        dz.expect_update_tenant_payment_status()
            .with(
                predicate::eq(tenant_pda),
                predicate::eq(TenantPaymentStatus::Delinquent),
            )
            .times(1)
            .returning(|_, _| Ok(Signature::new_unique()));

        let sentinel = new_sentinel(dz, sol).await;
        sentinel.deduct_tenant(&tenant, 10).await.unwrap();
    }

    #[tokio::test]
    async fn test_deduction_already_current() {
        // last_deduction_dz_epoch >= current_epoch → no calls
        let tenant = make_tenant_with_billing(
            1,
            2,
            TenantPaymentStatus::Paid,
            billing_config(1_000_000, 10),
        );

        let dz = MockDzRpcClientType::new();
        let sol = MockSolRpcClientType::new();
        // No expectations — mockall panics if any method is called

        let sentinel = new_sentinel(dz, sol).await;
        sentinel.deduct_tenant(&tenant, 10).await.unwrap();
    }

    #[tokio::test]
    async fn test_rate_zero_uses_legacy_path() {
        // rate == 0 → poll_cycle routes to check_and_update_tenant, NOT deduct_tenant
        let tenant =
            make_tenant_with_billing(1, 2, TenantPaymentStatus::Delinquent, billing_config(0, 0));
        let token_account = tenant.token_account;
        let tenant_pda = tenant.tenant_pda;

        let mut dz = MockDzRpcClientType::new();
        let mut sol = MockSolRpcClientType::new();

        dz.expect_get_current_dz_epoch().returning(|| Ok(10));

        dz.expect_get_tenants_with_token_accounts()
            .returning(move || {
                Ok(vec![make_tenant_with_billing(
                    1,
                    2,
                    TenantPaymentStatus::Delinquent,
                    billing_config(0, 0),
                )])
            });

        // Legacy balance check path
        sol.expect_get_token_account_balance()
            .with(predicate::eq(token_account))
            .times(1)
            .returning(|_| Ok(5000));

        dz.expect_update_tenant_payment_status()
            .with(
                predicate::eq(tenant_pda),
                predicate::eq(TenantPaymentStatus::Paid),
            )
            .times(1)
            .returning(|_, _| Ok(Signature::new_unique()));

        // transfer_spl_token should NOT be called (mockall panics if it is)

        let sentinel = new_sentinel(dz, sol).await;
        sentinel.poll_cycle().await.unwrap();
    }

    #[tokio::test]
    async fn test_deduction_cache_prevents_duplicate() {
        // First call: receipt doesn't exist, transfer + DZ tx succeed.
        // Second call: receipt check returns true → skip entirely.
        let tenant = make_tenant_with_billing(
            1,
            2,
            TenantPaymentStatus::Paid,
            billing_config(1_000_000, 5),
        );
        let tenant_pda = tenant.tenant_pda;
        let token_account = tenant.token_account;

        let mut dz = MockDzRpcClientType::new();
        let mut sol = MockSolRpcClientType::new();

        // First call: no receipt
        // Second call: receipt exists on-chain (covers the "already deducted" path)
        let mut receipt_seq = mockall::Sequence::new();
        dz.expect_billing_receipt_exists()
            .with(predicate::eq(tenant_pda), predicate::eq(6))
            .times(1)
            .in_sequence(&mut receipt_seq)
            .returning(|_, _| Ok(false));
        dz.expect_billing_receipt_exists()
            .with(predicate::eq(tenant_pda), predicate::eq(6))
            .times(1)
            .in_sequence(&mut receipt_seq)
            .returning(|_, _| Ok(true));

        // Transfer succeeds ONCE
        sol.expect_transfer_spl_token()
            .with(
                predicate::eq(token_account),
                predicate::eq(TEST_JOURNAL_ATA),
                predicate::eq(1_000_000),
            )
            .times(1)
            .returning(|_, _, _| Ok(Signature::new_unique()));

        // DZ tx called ONCE
        dz.expect_create_billing_receipt_and_update_epoch()
            .with(predicate::eq(tenant_pda), predicate::always())
            .times(1)
            .returning(|_, _| Ok(Signature::new_unique()));

        let sentinel = new_sentinel(dz, sol).await;

        // First call — deducts
        sentinel.deduct_tenant(&tenant, 10).await.unwrap();

        // Second call — receipt exists, no deduction (mockall panics if transfer called again)
        sentinel.deduct_tenant(&tenant, 10).await.unwrap();
    }

    #[tokio::test]
    async fn test_deduction_catches_up_one_epoch() {
        // Tenant is 3 epochs behind (last=5, current=8) → deducts only epoch 6
        let tenant = make_tenant_with_billing(
            1,
            2,
            TenantPaymentStatus::Paid,
            billing_config(1_000_000, 5),
        );
        let token_account = tenant.token_account;
        let tenant_pda = tenant.tenant_pda;

        let mut dz = MockDzRpcClientType::new();
        let mut sol = MockSolRpcClientType::new();

        // No receipt for epoch 6
        dz.expect_billing_receipt_exists()
            .with(predicate::eq(tenant_pda), predicate::eq(6))
            .times(1)
            .returning(|_, _| Ok(false));

        // Should transfer for epoch 6 only (last + 1)
        sol.expect_transfer_spl_token()
            .with(
                predicate::eq(token_account),
                predicate::eq(TEST_JOURNAL_ATA),
                predicate::eq(1_000_000),
            )
            .times(1)
            .returning(|_, _, _| Ok(Signature::new_unique()));

        // Should create receipt + bump to epoch 6, NOT 7 or 8
        dz.expect_create_billing_receipt_and_update_epoch()
            .with(predicate::eq(tenant_pda), predicate::always())
            .times(1)
            .returning(|_, _| Ok(Signature::new_unique()));

        let sentinel = new_sentinel(dz, sol).await;
        sentinel.deduct_tenant(&tenant, 8).await.unwrap();
    }

    #[tokio::test]
    async fn test_transfer_not_retried_after_epoch_update_failure() {
        // Transfer succeeds but DZ tx fails → second call retries
        // only the DZ tx (via transfer_cache), NOT the SPL transfer
        let tenant = make_tenant_with_billing(
            1,
            2,
            TenantPaymentStatus::Paid,
            billing_config(1_000_000, 5),
        );
        let tenant_pda = tenant.tenant_pda;
        let token_account = tenant.token_account;

        let mut dz = MockDzRpcClientType::new();
        let mut sol = MockSolRpcClientType::new();

        // Receipt checks:
        // 1. Pre-transfer on first call → false
        // 2. Post-DZ-failure on first call → false (receipt didn't land)
        // Second call hits transfer_cache so no pre-transfer check.
        dz.expect_billing_receipt_exists()
            .with(predicate::eq(tenant_pda), predicate::eq(6))
            .times(2)
            .returning(|_, _| Ok(false));

        // Transfer called exactly ONCE — must not be retried
        sol.expect_transfer_spl_token()
            .with(
                predicate::eq(token_account),
                predicate::eq(TEST_JOURNAL_ATA),
                predicate::eq(1_000_000),
            )
            .times(1)
            .returning(|_, _, _| Ok(Signature::new_unique()));

        // DZ tx: first call fails, second succeeds
        let mut seq = mockall::Sequence::new();

        dz.expect_create_billing_receipt_and_update_epoch()
            .with(predicate::eq(tenant_pda), predicate::always())
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Err(crate::Error::Deserialize("network error".into())));

        dz.expect_create_billing_receipt_and_update_epoch()
            .with(predicate::eq(tenant_pda), predicate::always())
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Ok(Signature::new_unique()));

        let sentinel = new_sentinel(dz, sol).await;

        // First call: transfer OK, DZ tx fails → error propagated
        assert!(sentinel.deduct_tenant(&tenant, 10).await.is_err());

        // Second call: transfer_cache hit → retries only DZ tx → succeeds
        // (mockall would panic if transfer_spl_token were called again)
        sentinel.deduct_tenant(&tenant, 10).await.unwrap();
    }

    #[tokio::test]
    async fn test_deduction_transient_error_does_not_set_delinquent() {
        // Transfer fails but balance >= rate → transient error, no status change
        let tenant = make_tenant_with_billing(
            1,
            2,
            TenantPaymentStatus::Paid,
            billing_config(1_000_000, 5),
        );
        let token_account = tenant.token_account;

        let mut dz = MockDzRpcClientType::new();
        let mut sol = MockSolRpcClientType::new();

        // No receipt exists
        dz.expect_billing_receipt_exists()
            .returning(|_, _| Ok(false));

        // Transfer fails
        sol.expect_transfer_spl_token()
            .times(1)
            .returning(|_, _, _| Err(crate::Error::Deserialize("timeout".into())));

        // Balance is sufficient — transient error
        sol.expect_get_token_account_balance()
            .with(predicate::eq(token_account))
            .times(1)
            .returning(|_| Ok(5_000_000));

        // update_tenant_payment_status should NOT be called
        // (mockall panics if it is)

        let sentinel = new_sentinel(dz, sol).await;
        sentinel.deduct_tenant(&tenant, 10).await.unwrap();
    }

    #[tokio::test]
    async fn test_receipt_exists_skips_deduction() {
        // Receipt already exists on DZ Ledger → no transfer, no DZ tx
        let tenant = make_tenant_with_billing(
            1,
            2,
            TenantPaymentStatus::Paid,
            billing_config(1_000_000, 5),
        );
        let tenant_pda = tenant.tenant_pda;

        let mut dz = MockDzRpcClientType::new();
        let sol = MockSolRpcClientType::new();

        // Receipt exists
        dz.expect_billing_receipt_exists()
            .with(predicate::eq(tenant_pda), predicate::eq(6))
            .times(1)
            .returning(|_, _| Ok(true));

        // No transfer or DZ tx expected (mockall panics if called)

        let sentinel = new_sentinel(dz, sol).await;
        sentinel.deduct_tenant(&tenant, 10).await.unwrap();
    }

    #[tokio::test]
    async fn test_transfer_cached_retry_dz_tx_only() {
        // Validates transfer_cache prevents re-transfer when DZ tx fails
        // within the same process run
        let tenant = make_tenant_with_billing(
            1,
            2,
            TenantPaymentStatus::Paid,
            billing_config(1_000_000, 5),
        );
        let tenant_pda = tenant.tenant_pda;
        let token_account = tenant.token_account;

        let mut dz = MockDzRpcClientType::new();
        let mut sol = MockSolRpcClientType::new();

        // Receipt checks:
        // 1. Pre-transfer on first call → false
        // 2. Post-DZ-failure on first call → false
        // 3. Post-DZ-failure on second call → false
        // Third call succeeds so no post-failure check.
        dz.expect_billing_receipt_exists()
            .with(predicate::eq(tenant_pda), predicate::eq(6))
            .times(3)
            .returning(|_, _| Ok(false));

        // Transfer only once
        sol.expect_transfer_spl_token()
            .with(
                predicate::eq(token_account),
                predicate::eq(TEST_JOURNAL_ATA),
                predicate::eq(1_000_000),
            )
            .times(1)
            .returning(|_, _, _| Ok(Signature::new_unique()));

        // DZ tx fails twice, succeeds third time
        let mut seq = mockall::Sequence::new();

        dz.expect_create_billing_receipt_and_update_epoch()
            .with(predicate::eq(tenant_pda), predicate::always())
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Err(crate::Error::Deserialize("DZ outage".into())));

        dz.expect_create_billing_receipt_and_update_epoch()
            .with(predicate::eq(tenant_pda), predicate::always())
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Err(crate::Error::Deserialize("DZ outage".into())));

        dz.expect_create_billing_receipt_and_update_epoch()
            .with(predicate::eq(tenant_pda), predicate::always())
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Ok(Signature::new_unique()));

        let sentinel = new_sentinel(dz, sol).await;

        // First call: transfer OK, DZ tx fails
        assert!(sentinel.deduct_tenant(&tenant, 10).await.is_err());

        // Second call: transfer_cache hit, DZ tx fails again
        assert!(sentinel.deduct_tenant(&tenant, 10).await.is_err());

        // Third call: transfer_cache hit, DZ tx succeeds
        sentinel.deduct_tenant(&tenant, 10).await.unwrap();
    }

    #[tokio::test]
    async fn test_ambiguous_dz_tx_success_treated_as_success() {
        // DZ tx returns an error but the receipt actually landed on-chain
        // (e.g. timeout or allocate-existing-account). Should treat as success.
        let tenant = make_tenant_with_billing(
            1,
            2,
            TenantPaymentStatus::Paid,
            billing_config(1_000_000, 5),
        );
        let tenant_pda = tenant.tenant_pda;
        let token_account = tenant.token_account;

        let mut dz = MockDzRpcClientType::new();
        let mut sol = MockSolRpcClientType::new();

        // No receipt before transfer
        dz.expect_billing_receipt_exists()
            .with(predicate::eq(tenant_pda), predicate::eq(6))
            .times(1)
            .returning(|_, _| Ok(false));

        // Transfer succeeds
        sol.expect_transfer_spl_token()
            .with(
                predicate::eq(token_account),
                predicate::eq(TEST_JOURNAL_ATA),
                predicate::eq(1_000_000),
            )
            .times(1)
            .returning(|_, _, _| Ok(Signature::new_unique()));

        // DZ tx fails with an error...
        dz.expect_create_billing_receipt_and_update_epoch()
            .with(predicate::eq(tenant_pda), predicate::always())
            .times(1)
            .returning(|_, _| Err(crate::Error::Deserialize("allocate: account exists".into())));

        // ...but the post-failure receipt check finds it on-chain
        dz.expect_billing_receipt_exists()
            .with(predicate::eq(tenant_pda), predicate::eq(6))
            .times(1)
            .returning(|_, _| Ok(true));

        let sentinel = new_sentinel(dz, sol).await;

        // Should succeed despite the DZ tx error
        sentinel.deduct_tenant(&tenant, 10).await.unwrap();
    }
}
