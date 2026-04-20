use std::{
    collections::HashMap,
    net::{IpAddr, Ipv4Addr},
    sync::Arc,
    time::Duration,
};

use async_trait::async_trait;
use solana_client::nonblocking::rpc_client::RpcClient;
use solana_sdk::{
    commitment_config::CommitmentConfig,
    hash::Hash,
    instruction::Instruction,
    message::{v0::Message, VersionedMessage},
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::VersionedTransaction,
};
use tokio::time::interval;
use tokio_util::sync::CancellationToken;
use tracing::{error, info};
use url::Url;

use crate::{
    dz_ledger_reader::DzUser,
    dz_ledger_writer::build_create_multicast_publisher_instructions,
    error::{rpc_with_retry, Result, SentinelError},
};

// ---------------------------------------------------------------------------
// Traits
// ---------------------------------------------------------------------------

/// Reads DZ Ledger state for the multicast publisher sentinel.
#[cfg_attr(test, mockall::automock)]
#[async_trait]
pub trait MulticastDzLedgerClient: Send + Sync {
    async fn fetch_all_dz_users(&self) -> Result<Vec<DzUser>>;
    async fn create_multicast_publisher(&self, mgroup_pk: &Pubkey, user: &DzUser) -> Result<()>;
}

/// Provides the list of active validators by IP for the sentinel.
#[cfg_attr(test, mockall::automock)]
#[async_trait]
pub trait ValidatorListReader: Send + Sync {
    async fn fetch_validators(&self) -> Result<HashMap<Ipv4Addr, ValidatorStake>>;
}

/// Minimal validator info needed by the sentinel.
#[derive(Debug, Clone)]
pub struct ValidatorStake {
    pub activated_stake: i64,
    pub software_client: String,
}

// ---------------------------------------------------------------------------
// Sentinel
// ---------------------------------------------------------------------------

pub struct MulticastPublisherSentinel<D: MulticastDzLedgerClient, V: ValidatorListReader> {
    dz_client: D,
    validator_list_reader: V,
    metadata_api_url: Option<String>,
    multicast_group_pubkeys: Vec<Pubkey>,
    client_filters: Vec<String>,
    poll_interval: Duration,
}

impl<D: MulticastDzLedgerClient, V: ValidatorListReader> MulticastPublisherSentinel<D, V> {
    pub fn with_clients(
        dz_client: D,
        validator_list_reader: V,
        multicast_group_pubkeys: Vec<Pubkey>,
        client_filters: Vec<String>,
        metadata_api_url: Option<String>,
        poll_interval_secs: u64,
    ) -> Self {
        Self {
            dz_client,
            validator_list_reader,
            metadata_api_url,
            multicast_group_pubkeys,
            client_filters,
            poll_interval: Duration::from_secs(poll_interval_secs),
        }
    }

    pub async fn run(&self, shutdown_listener: CancellationToken) -> Result<()> {
        if self.multicast_group_pubkeys.is_empty() {
            info!("multicast publisher sentinel disabled (no group pubkeys configured)");
            shutdown_listener.cancelled().await;
            return Ok(());
        }

        info!(
            groups = ?self.multicast_group_pubkeys,
            poll_interval_secs = self.poll_interval.as_secs(),
            "multicast publisher sentinel starting"
        );

        let mut poll_timer = interval(self.poll_interval);

        loop {
            tokio::select! {
                biased;
                _ = shutdown_listener.cancelled() => {
                    info!("multicast publisher sentinel: shutdown signal received");
                    break;
                }
                _ = poll_timer.tick() => {
                    if let Err(err) = self.poll_cycle().await {
                        error!(?err, "multicast publisher poll cycle failed; will retry next cycle");
                        metrics::counter!("doublezero_sentinel_multicast_pub_poll_failed").increment(1);
                    }
                }
            }
        }

        Ok(())
    }

    pub async fn poll_cycle(&self) -> Result<()> {
        let mut validators = self.validator_list_reader.fetch_validators().await?;
        if validators.is_empty() {
            return Ok(());
        }

        // If client_filters is set, enrich validators with software_client from the
        // metadata API so we can filter. This must succeed — returning unfiltered
        // data when filtering was requested is not acceptable.
        if !self.client_filters.is_empty() {
            let needs_enrichment = validators.values().any(|v| v.software_client.is_empty());
            if needs_enrichment {
                let metadata_url = self.metadata_api_url.as_deref().ok_or_else(|| {
                    SentinelError::Deserialize(
                        "client_filter is set but no --validator-metadata-url configured \
                         for software client enrichment"
                            .into(),
                    )
                })?;

                let clients = fetch_software_clients(metadata_url).await.map_err(|e| {
                    SentinelError::Deserialize(format!(
                        "failed to fetch validator metadata for client enrichment: {e}"
                    ))
                })?;

                for (ip, vs) in validators.iter_mut() {
                    if vs.software_client.is_empty() {
                        if let Some(client) = clients.get(ip) {
                            vs.software_client.clone_from(client);
                        }
                    }
                }
            }
        }

        let all_users = self.dz_client.fetch_all_dz_users().await?;

        let ibrl_users: Vec<_> = all_users
            .iter()
            .filter(|u| {
                u.user_type == doublezero_sdk::UserType::IBRL
                    || u.user_type == doublezero_sdk::UserType::IBRLWithAllocatedIP
            })
            .collect();

        for mgroup_pk in &self.multicast_group_pubkeys {
            let publisher_ips: std::collections::HashSet<Ipv4Addr> = all_users
                .iter()
                .filter(|u| {
                    u.user_type == doublezero_sdk::UserType::Multicast
                        && u.publishers.contains(mgroup_pk)
                })
                .map(|u| u.client_ip)
                .collect();

            let mut candidates: Vec<&DzUser> = ibrl_users
                .iter()
                .filter(|u| {
                    if !validators.contains_key(&u.client_ip)
                        || publisher_ips.contains(&u.client_ip)
                    {
                        return false;
                    }
                    if !self.client_filters.is_empty() {
                        if let Some(v) = validators.get(&u.client_ip) {
                            let name = v.software_client.to_lowercase();
                            return self
                                .client_filters
                                .iter()
                                .any(|f| name.contains(&f.to_lowercase()));
                        }
                    }
                    true
                })
                .copied()
                .collect();

            if candidates.is_empty() {
                continue;
            }

            candidates.sort_by(|a, b| {
                let stake_a = validators
                    .get(&a.client_ip)
                    .map(|v| v.activated_stake)
                    .unwrap_or(0);
                let stake_b = validators
                    .get(&b.client_ip)
                    .map(|v| v.activated_stake)
                    .unwrap_or(0);
                stake_b.cmp(&stake_a)
            });

            info!(
                group = %mgroup_pk,
                candidates = candidates.len(),
                existing_publishers = publisher_ips.len(),
                "found validators needing multicast publisher"
            );

            metrics::gauge!(
                "doublezero_sentinel_multicast_pub_candidates",
                "group" => mgroup_pk.to_string()
            )
            .set(candidates.len() as f64);

            for user in candidates {
                if let Err(err) = self
                    .dz_client
                    .create_multicast_publisher(mgroup_pk, user)
                    .await
                {
                    error!(
                        ?err,
                        ip = %user.client_ip,
                        device = %user.device_pk,
                        group = %mgroup_pk,
                        "failed to create multicast publisher"
                    );
                    metrics::counter!(
                        "doublezero_sentinel_multicast_pub_create_failed",
                        "group" => mgroup_pk.to_string()
                    )
                    .increment(1);
                } else {
                    info!(
                        ip = %user.client_ip,
                        device = %user.device_pk,
                        group = %mgroup_pk,
                        "created multicast publisher"
                    );
                    metrics::counter!(
                        "doublezero_sentinel_multicast_pub_created",
                        "group" => mgroup_pk.to_string()
                    )
                    .increment(1);
                }
            }
        }

        Ok(())
    }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

fn new_transaction(
    instructions: &[Instruction],
    signers: &[&Keypair],
    recent_blockhash: Hash,
) -> Result<VersionedTransaction> {
    let message =
        Message::try_compile(&signers[0].pubkey(), instructions, &[], recent_blockhash)
            .map_err(|e| SentinelError::Deserialize(format!("compile transaction message: {e}")))?;

    VersionedTransaction::try_new(VersionedMessage::V0(message), signers)
        .map_err(|e| SentinelError::Deserialize(format!("sign transaction: {e}")))
}

/// Fetch software_client info from the validator metadata service for enrichment.
async fn fetch_software_clients(
    url: &str,
) -> std::result::Result<HashMap<Ipv4Addr, String>, Box<dyn std::error::Error + Send + Sync>> {
    #[derive(serde::Deserialize)]
    struct Item {
        ip: String,
        software_client: String,
    }

    let client = reqwest::Client::new();
    let resp = client.get(url).send().await?;
    if !resp.status().is_success() {
        let body = resp.text().await.unwrap_or_default();
        return Err(format!("metadata service error: {body}").into());
    }
    let items: Vec<Item> = resp.json().await?;
    let mut map = HashMap::new();
    for item in items {
        if let Ok(ip) = item.ip.parse() {
            map.insert(ip, item.software_client);
        }
    }
    Ok(map)
}

// ---------------------------------------------------------------------------
// Concrete implementations
// ---------------------------------------------------------------------------

pub struct RpcMulticastDzLedgerClient {
    rpc_client: RpcClient,
    payer: Arc<Keypair>,
    serviceability_id: Pubkey,
}

impl RpcMulticastDzLedgerClient {
    pub fn new(dz_rpc_url: Url, payer: Arc<Keypair>, serviceability_id: Pubkey) -> Self {
        Self {
            rpc_client: RpcClient::new_with_commitment(
                dz_rpc_url.into(),
                CommitmentConfig::confirmed(),
            ),
            payer,
            serviceability_id,
        }
    }
}

#[async_trait]
impl MulticastDzLedgerClient for RpcMulticastDzLedgerClient {
    async fn fetch_all_dz_users(&self) -> Result<Vec<DzUser>> {
        use doublezero_sdk::{AccountData, AccountType, UserStatus};
        use solana_account_decoder::UiAccountEncoding;
        use solana_client::{
            rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
            rpc_filter::{Memcmp, RpcFilterType},
        };

        let user_type_byte = AccountType::User as u8;
        let accounts = self
            .rpc_client
            .get_program_accounts_with_config(
                &self.serviceability_id,
                RpcProgramAccountsConfig {
                    filters: Some(vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                        0,
                        vec![user_type_byte],
                    ))]),
                    account_config: RpcAccountInfoConfig {
                        encoding: Some(UiAccountEncoding::Base64),
                        ..Default::default()
                    },
                    ..Default::default()
                },
            )
            .await?;

        let mut users = Vec::new();
        for (_pk, account) in accounts {
            let Ok(ad) = AccountData::try_from(account.data.as_slice()) else {
                continue;
            };
            let Ok(user) = ad.get_user() else {
                continue;
            };
            if user.status != UserStatus::Activated {
                continue;
            }
            if user.user_type != doublezero_sdk::UserType::IBRL
                && user.user_type != doublezero_sdk::UserType::IBRLWithAllocatedIP
                && user.user_type != doublezero_sdk::UserType::Multicast
            {
                continue;
            }
            users.push(DzUser {
                owner: user.owner,
                client_ip: user.client_ip,
                device_pk: user.device_pk,
                tenant_pk: user.tenant_pk,
                user_type: user.user_type,
                publishers: user.publishers.clone(),
            });
        }

        metrics::gauge!("doublezero_sentinel_multicast_pub_ibrl_users").set(
            users
                .iter()
                .filter(|u| {
                    u.user_type == doublezero_sdk::UserType::IBRL
                        || u.user_type == doublezero_sdk::UserType::IBRLWithAllocatedIP
                })
                .count() as f64,
        );

        Ok(users)
    }

    async fn create_multicast_publisher(&self, mgroup_pk: &Pubkey, user: &DzUser) -> Result<()> {
        let payer_pk = self.payer.pubkey();
        let ixs = build_create_multicast_publisher_instructions(
            &self.serviceability_id,
            &payer_pk,
            &user.owner,
            mgroup_pk,
            user,
        )
        .map_err(|e| SentinelError::Deserialize(format!("build multicast publisher ixs: {e}")))?;

        // Step 1: set_access_pass
        rpc_with_retry(
            || async {
                let blockhash = self.rpc_client.get_latest_blockhash().await?;
                let tx = new_transaction(
                    std::slice::from_ref(&ixs.set_access_pass),
                    &[&self.payer],
                    blockhash,
                )?;
                let sig = self.rpc_client.send_and_confirm_transaction(&tx).await?;
                info!(ip = %user.client_ip, %sig, "set_access_pass");
                Ok(())
            },
            "multicast_pub: set_access_pass",
        )
        .await?;

        // Step 2: add_multicast_publisher_allowlist
        rpc_with_retry(
            || async {
                let blockhash = self.rpc_client.get_latest_blockhash().await?;
                let tx = new_transaction(std::slice::from_ref(&ixs.add_allowlist), &[&self.payer], blockhash)?;
                let sig = self.rpc_client.send_and_confirm_transaction(&tx).await?;
                info!(ip = %user.client_ip, group = %mgroup_pk, %sig, "add_multicast_pub_allowlist");
                Ok(())
            },
            "multicast_pub: add_pub_allowlist",
        )
        .await?;

        // Step 3: create_subscribe_user (as publisher)
        rpc_with_retry(
            || async {
                let blockhash = self.rpc_client.get_latest_blockhash().await?;
                let tx = new_transaction(std::slice::from_ref(&ixs.create_user), &[&self.payer], blockhash)?;
                let sig = self.rpc_client.send_and_confirm_transaction(&tx).await?;
                info!(ip = %user.client_ip, device = %user.device_pk, %sig, "create_multicast_publisher");
                Ok(())
            },
            "multicast_pub: create_subscribe_user",
        )
        .await?;

        Ok(())
    }
}

/// Validator list reader backed by Solana RPC (get_vote_accounts + get_cluster_nodes).
pub struct SolanaRpcValidatorListReader {
    rpc_client: RpcClient,
}

impl SolanaRpcValidatorListReader {
    pub fn new(solana_rpc_url: Url) -> Self {
        Self {
            rpc_client: RpcClient::new_with_commitment(
                solana_rpc_url.into(),
                CommitmentConfig::confirmed(),
            ),
        }
    }
}

#[async_trait]
impl ValidatorListReader for SolanaRpcValidatorListReader {
    async fn fetch_validators(&self) -> Result<HashMap<Ipv4Addr, ValidatorStake>> {
        // Step 1: Get vote accounts → node_pubkey → activated_stake
        let vote_accounts = self.rpc_client.get_vote_accounts().await?;
        let mut node_stakes: HashMap<String, u64> = HashMap::new();
        for va in &vote_accounts.current {
            node_stakes.insert(va.node_pubkey.clone(), va.activated_stake);
        }

        // Step 2: Get cluster nodes → node_pubkey → gossip IP
        let cluster_nodes = self.rpc_client.get_cluster_nodes().await?;

        // Step 3: Join on node_pubkey to build IP → stake map
        let mut map = HashMap::new();
        for node in &cluster_nodes {
            if let Some(&stake) = node_stakes.get(&node.pubkey) {
                if let Some(gossip) = &node.gossip {
                    if let IpAddr::V4(ipv4) = gossip.ip() {
                        map.insert(
                            ipv4,
                            ValidatorStake {
                                activated_stake: stake as i64,
                                software_client: String::new(),
                            },
                        );
                    }
                }
            }
        }

        metrics::gauge!("doublezero_sentinel_multicast_pub_dz_validators").set(map.len() as f64);

        Ok(map)
    }
}

// ---------------------------------------------------------------------------
// Convenience constructor for production use
// ---------------------------------------------------------------------------

impl MulticastPublisherSentinel<RpcMulticastDzLedgerClient, SolanaRpcValidatorListReader> {
    #[allow(clippy::too_many_arguments)]
    pub fn new(
        dz_rpc_url: Url,
        solana_rpc_url: Url,
        payer: Arc<Keypair>,
        serviceability_id: Pubkey,
        multicast_group_pubkeys: Vec<Pubkey>,
        client_filters: Vec<String>,
        validator_metadata_url: Option<String>,
        poll_interval_secs: u64,
    ) -> Self {
        Self::with_clients(
            RpcMulticastDzLedgerClient::new(dz_rpc_url, payer, serviceability_id),
            SolanaRpcValidatorListReader::new(solana_rpc_url),
            multicast_group_pubkeys,
            client_filters,
            validator_metadata_url,
            poll_interval_secs,
        )
    }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use std::collections::HashSet;

    use doublezero_sdk::UserType;
    use solana_sdk::pubkey::Pubkey;

    use super::*;

    fn make_ibrl_user(ip: [u8; 4], device_pk: Pubkey) -> DzUser {
        DzUser {
            owner: Pubkey::default(),
            client_ip: Ipv4Addr::from(ip),
            device_pk,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            publishers: vec![],
        }
    }

    fn make_ibrl_with_allocated_ip_user(ip: [u8; 4], device_pk: Pubkey) -> DzUser {
        DzUser {
            owner: Pubkey::default(),
            client_ip: Ipv4Addr::from(ip),
            device_pk,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRLWithAllocatedIP,
            publishers: vec![],
        }
    }

    fn make_multicast_publisher(ip: [u8; 4], groups: Vec<Pubkey>) -> DzUser {
        DzUser {
            owner: Pubkey::default(),
            client_ip: Ipv4Addr::from(ip),
            device_pk: Pubkey::new_unique(),
            tenant_pk: Pubkey::default(),
            user_type: UserType::Multicast,
            publishers: groups,
        }
    }

    fn make_sentinel_with_filter(
        dz: MockMulticastDzLedgerClient,
        api: MockValidatorListReader,
        groups: Vec<Pubkey>,
        client_filters: Vec<String>,
    ) -> MulticastPublisherSentinel<MockMulticastDzLedgerClient, MockValidatorListReader> {
        MulticastPublisherSentinel::with_clients(dz, api, groups, client_filters, None, 300)
    }

    fn make_sentinel(
        dz: MockMulticastDzLedgerClient,
        api: MockValidatorListReader,
        groups: Vec<Pubkey>,
    ) -> MulticastPublisherSentinel<MockMulticastDzLedgerClient, MockValidatorListReader> {
        make_sentinel_with_filter(dz, api, groups, vec![])
    }

    #[tokio::test]
    async fn no_validators_returns_early() {
        let mut api = MockValidatorListReader::new();
        api.expect_fetch_validators()
            .returning(|| Ok(HashMap::new()));

        let mut dz = MockMulticastDzLedgerClient::new();
        dz.expect_fetch_all_dz_users().never();
        dz.expect_create_multicast_publisher().never();

        let group = Pubkey::new_unique();
        let sentinel = make_sentinel(dz, api, vec![group]);
        sentinel.poll_cycle().await.unwrap();
    }

    #[tokio::test]
    async fn no_ibrl_users_no_candidates() {
        let mut api = MockValidatorListReader::new();
        let mut validators = HashMap::new();
        validators.insert(
            Ipv4Addr::new(10, 0, 0, 1),
            ValidatorStake {
                activated_stake: 100,
                software_client: "JitoLabs".into(),
            },
        );
        api.expect_fetch_validators()
            .returning(move || Ok(validators.clone()));

        let mut dz = MockMulticastDzLedgerClient::new();
        dz.expect_fetch_all_dz_users()
            .returning(|| Ok(vec![make_multicast_publisher([10, 0, 0, 1], vec![])]));
        dz.expect_create_multicast_publisher().never();

        let group = Pubkey::new_unique();
        let sentinel = make_sentinel(dz, api, vec![group]);
        sentinel.poll_cycle().await.unwrap();
    }

    #[tokio::test]
    async fn existing_publishers_are_skipped() {
        let group = Pubkey::new_unique();
        let ip = [10, 0, 0, 1];

        let mut api = MockValidatorListReader::new();
        let mut validators = HashMap::new();
        validators.insert(
            Ipv4Addr::from(ip),
            ValidatorStake {
                activated_stake: 100,
                software_client: "JitoLabs".into(),
            },
        );
        api.expect_fetch_validators()
            .returning(move || Ok(validators.clone()));

        let group_clone = group;
        let mut dz = MockMulticastDzLedgerClient::new();
        dz.expect_fetch_all_dz_users().returning(move || {
            Ok(vec![
                make_ibrl_user(ip, Pubkey::new_unique()),
                make_multicast_publisher(ip, vec![group_clone]),
            ])
        });
        dz.expect_create_multicast_publisher().never();

        let sentinel = make_sentinel(dz, api, vec![group]);
        sentinel.poll_cycle().await.unwrap();
    }

    #[tokio::test]
    async fn finds_correct_candidates() {
        let group = Pubkey::new_unique();
        let ip1 = [10, 0, 0, 1];
        let ip2 = [10, 0, 0, 2];
        let ip3 = [10, 0, 0, 3]; // not a validator

        let device2 = Pubkey::new_unique();

        let mut api = MockValidatorListReader::new();
        let mut validators = HashMap::new();
        validators.insert(
            Ipv4Addr::from(ip1),
            ValidatorStake {
                activated_stake: 100,
                software_client: "JitoLabs".into(),
            },
        );
        validators.insert(
            Ipv4Addr::from(ip2),
            ValidatorStake {
                activated_stake: 200,
                software_client: "JitoLabs".into(),
            },
        );
        api.expect_fetch_validators()
            .returning(move || Ok(validators.clone()));

        let group_clone = group;
        let device2_clone = device2;
        let mut dz = MockMulticastDzLedgerClient::new();
        dz.expect_fetch_all_dz_users().returning(move || {
            Ok(vec![
                make_ibrl_user(ip1, Pubkey::new_unique()),
                make_ibrl_user(ip2, device2_clone),
                make_ibrl_user(ip3, Pubkey::new_unique()),
                make_multicast_publisher(ip1, vec![group_clone]),
            ])
        });

        // Only ip2 should be created.
        dz.expect_create_multicast_publisher()
            .withf(move |g, u| {
                *g == group && u.client_ip == Ipv4Addr::from(ip2) && u.device_pk == device2
            })
            .times(1)
            .returning(|_, _| Ok(()));

        let sentinel = make_sentinel(dz, api, vec![group]);
        sentinel.poll_cycle().await.unwrap();
    }

    #[tokio::test]
    async fn creation_error_does_not_stop_other_candidates() {
        let group = Pubkey::new_unique();
        let ip1 = [10, 0, 0, 1];
        let ip2 = [10, 0, 0, 2];

        let mut api = MockValidatorListReader::new();
        let mut validators = HashMap::new();
        validators.insert(
            Ipv4Addr::from(ip1),
            ValidatorStake {
                activated_stake: 100,
                software_client: "JitoLabs".into(),
            },
        );
        validators.insert(
            Ipv4Addr::from(ip2),
            ValidatorStake {
                activated_stake: 200,
                software_client: "JitoLabs".into(),
            },
        );
        api.expect_fetch_validators()
            .returning(move || Ok(validators.clone()));

        let mut dz = MockMulticastDzLedgerClient::new();
        dz.expect_fetch_all_dz_users().returning(move || {
            Ok(vec![
                make_ibrl_user(ip1, Pubkey::new_unique()),
                make_ibrl_user(ip2, Pubkey::new_unique()),
            ])
        });

        let mut seq = mockall::Sequence::new();
        // First call (ip2, higher stake) fails.
        dz.expect_create_multicast_publisher()
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Err(SentinelError::Deserialize("simulated failure".into())));
        // Second call (ip1) should still happen.
        dz.expect_create_multicast_publisher()
            .times(1)
            .in_sequence(&mut seq)
            .returning(|_, _| Ok(()));

        let sentinel = make_sentinel(dz, api, vec![group]);
        sentinel.poll_cycle().await.unwrap();
    }

    #[tokio::test]
    async fn multiple_groups_processed_independently() {
        let group_a = Pubkey::new_unique();
        let group_b = Pubkey::new_unique();
        let ip = [10, 0, 0, 1];

        let mut api = MockValidatorListReader::new();
        let mut validators = HashMap::new();
        validators.insert(
            Ipv4Addr::from(ip),
            ValidatorStake {
                activated_stake: 100,
                software_client: "JitoLabs".into(),
            },
        );
        api.expect_fetch_validators()
            .returning(move || Ok(validators.clone()));

        let group_a_clone = group_a;
        let mut dz = MockMulticastDzLedgerClient::new();
        dz.expect_fetch_all_dz_users().returning(move || {
            Ok(vec![
                make_ibrl_user(ip, Pubkey::new_unique()),
                make_multicast_publisher(ip, vec![group_a_clone]),
            ])
        });

        // Should only create for group B.
        dz.expect_create_multicast_publisher()
            .withf(move |g, _| *g == group_b)
            .times(1)
            .returning(|_, _| Ok(()));

        let sentinel = make_sentinel(dz, api, vec![group_a, group_b]);
        sentinel.poll_cycle().await.unwrap();
    }

    #[tokio::test]
    async fn validator_list_error_propagates() {
        let mut api = MockValidatorListReader::new();
        api.expect_fetch_validators().returning(|| {
            Err(SentinelError::Deserialize(
                "validator list source unavailable".into(),
            ))
        });

        let dz = MockMulticastDzLedgerClient::new();

        let group = Pubkey::new_unique();
        let sentinel = make_sentinel(dz, api, vec![group]);
        let result = sentinel.poll_cycle().await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn user_fetch_error_propagates() {
        let mut api = MockValidatorListReader::new();
        let mut validators = HashMap::new();
        validators.insert(
            Ipv4Addr::new(10, 0, 0, 1),
            ValidatorStake {
                activated_stake: 100,
                software_client: "JitoLabs".into(),
            },
        );
        api.expect_fetch_validators()
            .returning(move || Ok(validators.clone()));

        let mut dz = MockMulticastDzLedgerClient::new();
        dz.expect_fetch_all_dz_users()
            .returning(|| Err(SentinelError::Deserialize("RPC down".into())));

        let group = Pubkey::new_unique();
        let sentinel = make_sentinel(dz, api, vec![group]);
        let result = sentinel.poll_cycle().await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn client_filter_only_creates_for_matching_validators() {
        let group = Pubkey::new_unique();
        let ip_jito = [10, 0, 0, 1];
        let ip_agave = [10, 0, 0, 2];

        let mut api = MockValidatorListReader::new();
        let mut validators = HashMap::new();
        validators.insert(
            Ipv4Addr::from(ip_jito),
            ValidatorStake {
                activated_stake: 100,
                software_client: "JitoLabs".into(),
            },
        );
        validators.insert(
            Ipv4Addr::from(ip_agave),
            ValidatorStake {
                activated_stake: 200,
                software_client: "Agave".into(),
            },
        );
        api.expect_fetch_validators()
            .returning(move || Ok(validators.clone()));

        let mut dz = MockMulticastDzLedgerClient::new();
        dz.expect_fetch_all_dz_users().returning(move || {
            Ok(vec![
                make_ibrl_user(ip_jito, Pubkey::new_unique()),
                make_ibrl_user(ip_agave, Pubkey::new_unique()),
            ])
        });

        // Only JitoLabs validator should be created.
        dz.expect_create_multicast_publisher()
            .withf(move |_, u| u.client_ip == Ipv4Addr::from(ip_jito))
            .times(1)
            .returning(|_, _| Ok(()));

        let sentinel = make_sentinel_with_filter(dz, api, vec![group], vec!["JitoLabs".into()]);
        sentinel.poll_cycle().await.unwrap();
    }

    #[tokio::test]
    async fn multiple_client_filters_match_any() {
        let group = Pubkey::new_unique();
        let ip_jito = [10, 0, 0, 1];
        let ip_agave = [10, 0, 0, 2];
        let ip_frank = [10, 0, 0, 3];

        let mut api = MockValidatorListReader::new();
        let mut validators = HashMap::new();
        validators.insert(
            Ipv4Addr::from(ip_jito),
            ValidatorStake {
                activated_stake: 300,
                software_client: "JitoLabs".into(),
            },
        );
        validators.insert(
            Ipv4Addr::from(ip_agave),
            ValidatorStake {
                activated_stake: 200,
                software_client: "Agave".into(),
            },
        );
        validators.insert(
            Ipv4Addr::from(ip_frank),
            ValidatorStake {
                activated_stake: 100,
                software_client: "Frankendancer".into(),
            },
        );
        api.expect_fetch_validators()
            .returning(move || Ok(validators.clone()));

        let mut dz = MockMulticastDzLedgerClient::new();
        dz.expect_fetch_all_dz_users().returning(move || {
            Ok(vec![
                make_ibrl_user(ip_jito, Pubkey::new_unique()),
                make_ibrl_user(ip_agave, Pubkey::new_unique()),
                make_ibrl_user(ip_frank, Pubkey::new_unique()),
            ])
        });

        // Both JitoLabs and Frankendancer should be created; Agave skipped.
        let created_ips = Arc::new(std::sync::Mutex::new(HashSet::new()));
        let created_ips_clone = created_ips.clone();
        dz.expect_create_multicast_publisher()
            .times(2)
            .returning(move |_, u| {
                created_ips_clone.lock().unwrap().insert(u.client_ip);
                Ok(())
            });

        let sentinel = make_sentinel_with_filter(
            dz,
            api,
            vec![group],
            vec!["jito".into(), "Frankendancer".into()],
        );
        sentinel.poll_cycle().await.unwrap();

        let ips = created_ips.lock().unwrap();
        assert!(ips.contains(&Ipv4Addr::from(ip_jito)));
        assert!(ips.contains(&Ipv4Addr::from(ip_frank)));
        assert!(!ips.contains(&Ipv4Addr::from(ip_agave)));
    }

    #[tokio::test]
    async fn client_filter_without_metadata_url_errors() {
        // Solana RPC source: validators have empty software_client.
        // Without a metadata_api_url, enrichment fails and poll_cycle returns an error
        // rather than returning unfiltered data.
        let group = Pubkey::new_unique();
        let ip = [10, 0, 0, 1];

        let mut api = MockValidatorListReader::new();
        let mut validators = HashMap::new();
        validators.insert(
            Ipv4Addr::from(ip),
            ValidatorStake {
                activated_stake: 100,
                software_client: String::new(),
            },
        );
        api.expect_fetch_validators()
            .returning(move || Ok(validators.clone()));

        let dz = MockMulticastDzLedgerClient::new();

        let sentinel = MulticastPublisherSentinel::with_clients(
            dz,
            api,
            vec![group],
            vec!["JitoLabs".into()],
            None,
            300,
        );
        let result = sentinel.poll_cycle().await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn no_client_filter_creates_all_validators() {
        // Without client_filter, all validators are candidates regardless of
        // software_client value (even if empty from Solana RPC source).
        let group = Pubkey::new_unique();
        let ip1 = [10, 0, 0, 1];
        let ip2 = [10, 0, 0, 2];

        let mut api = MockValidatorListReader::new();
        let mut validators = HashMap::new();
        validators.insert(
            Ipv4Addr::from(ip1),
            ValidatorStake {
                activated_stake: 200,
                software_client: String::new(),
            },
        );
        validators.insert(
            Ipv4Addr::from(ip2),
            ValidatorStake {
                activated_stake: 100,
                software_client: String::new(),
            },
        );
        api.expect_fetch_validators()
            .returning(move || Ok(validators.clone()));

        let mut dz = MockMulticastDzLedgerClient::new();
        dz.expect_fetch_all_dz_users().returning(move || {
            Ok(vec![
                make_ibrl_user(ip1, Pubkey::new_unique()),
                make_ibrl_user(ip2, Pubkey::new_unique()),
            ])
        });

        // Both should be created (no client filter).
        let created_ips = Arc::new(std::sync::Mutex::new(HashSet::new()));
        let created_ips_clone = created_ips.clone();
        dz.expect_create_multicast_publisher()
            .times(2)
            .returning(move |_, u| {
                created_ips_clone.lock().unwrap().insert(u.client_ip);
                Ok(())
            });

        let sentinel = make_sentinel(dz, api, vec![group]);
        sentinel.poll_cycle().await.unwrap();

        let ips = created_ips.lock().unwrap();
        assert!(ips.contains(&Ipv4Addr::from(ip1)));
        assert!(ips.contains(&Ipv4Addr::from(ip2)));
    }

    #[tokio::test]
    async fn ibrl_with_allocated_ip_users_are_candidates() {
        let group = Pubkey::new_unique();
        let ip = [10, 0, 0, 1];
        let device = Pubkey::new_unique();

        let mut api = MockValidatorListReader::new();
        let mut validators = HashMap::new();
        validators.insert(
            Ipv4Addr::from(ip),
            ValidatorStake {
                activated_stake: 100,
                software_client: String::new(),
            },
        );
        api.expect_fetch_validators()
            .returning(move || Ok(validators.clone()));

        let device_clone = device;
        let mut dz = MockMulticastDzLedgerClient::new();
        dz.expect_fetch_all_dz_users()
            .returning(move || Ok(vec![make_ibrl_with_allocated_ip_user(ip, device_clone)]));

        dz.expect_create_multicast_publisher()
            .withf(move |g, u| {
                *g == group
                    && u.client_ip == Ipv4Addr::from(ip)
                    && u.device_pk == device
                    && u.user_type == UserType::IBRLWithAllocatedIP
            })
            .times(1)
            .returning(|_, _| Ok(()));

        let sentinel = make_sentinel(dz, api, vec![group]);
        sentinel.poll_cycle().await.unwrap();
    }
}
