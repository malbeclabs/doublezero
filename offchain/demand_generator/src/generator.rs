use crate::{
    city_aggregator::aggregate_by_city,
    demand_matrix::{DemandConfig, generate_demand_matrix},
    settings::Settings,
    types::{EnrichedValidator, IpInfoResp, ValidatorDetail, ValidatorIpMap},
};
use anyhow::{Context, Result, bail};
use network_shapley::types::Demand;
use solana_client::{
    nonblocking::rpc_client::RpcClient,
    rpc_config::RpcGetVoteAccountsConfig,
    rpc_response::{RpcContactInfo, RpcVoteAccountInfo},
};
use solana_sdk::pubkey::Pubkey;
use std::{
    collections::{HashMap, HashSet},
    sync::Arc,
};
use tokio::{
    sync::Semaphore,
    task::JoinSet,
    time::{Duration, sleep},
};
use tracing::{debug, info, warn};

#[derive(Debug)]
pub struct DemandGenerator {
    settings: Settings,
}

impl DemandGenerator {
    pub fn new(settings: Settings) -> Self {
        Self { settings }
    }

    pub async fn generate(&self) -> Result<Vec<Demand>> {
        let (_, demands) = self.generate_with_validators().await?;
        Ok(demands)
    }

    pub async fn generate_with_validators(&self) -> Result<(Vec<EnrichedValidator>, Vec<Demand>)> {
        let rpc_client = RpcClient::new(self.settings.demand_generator.solana_rpc_url.clone());
        let vote_accounts = get_all_validator_identities(&rpc_client).await?;

        // Build stake map
        let mut stake_map: HashMap<Pubkey, u64> = HashMap::new();
        for vote_account in &vote_accounts {
            if let Ok(identity_pubkey) = vote_account.node_pubkey.parse::<Pubkey>() {
                *stake_map.entry(identity_pubkey).or_insert(0) += vote_account.activated_stake;
            }
        }

        // Count unique validators
        let unique_validator_count = stake_map.len();
        info!("Unique validator identities: {}", unique_validator_count);

        // Get all cluster nodes
        let cluster_nodes = get_cluster_nodes(&rpc_client).await?;

        // Convert the list of nodes into a map for lookups
        // A single validator can have multiple vote accounts. We only want one entry per validator identity.
        let ip_map = ip_map(cluster_nodes);

        // Combine to construct Vec<ValidatorDetails>
        let validators_with_gossip =
            filter_gossiping_validators(&ip_map, &vote_accounts, &stake_map)?;
        info!("Gossiping validators: {}", validators_with_gossip.len());

        // Add ip info data
        let enriched_validators =
            enrich_validators(&self.settings, &validators_with_gossip).await?;
        info!("Enriched validators: {}", enriched_validators.len());

        // Aggregate validators by city
        let city_aggregates = aggregate_by_city(&enriched_validators)?;
        info!("Stake aggregated into cities: {}", city_aggregates.len());

        // Generate demand matrix
        let config = DemandConfig::default();
        let demands = generate_demand_matrix(&city_aggregates, &config)?;
        info!("Generated demand entries: {}", demands.len());

        Ok((enriched_validators, demands))
    }
}

/// Retry an operation with exponential backoff and jitter
async fn with_retry<T, F, Fut>(operation: F, settings: &Settings, operation_name: &str) -> Result<T>
where
    F: Fn() -> Fut,
    Fut: std::future::Future<Output = Result<T>>,
{
    let mut retry_count = 0;
    let mut backoff = Duration::from_millis(settings.demand_generator.retry_backoff_base_ms);
    let max_backoff = Duration::from_millis(settings.demand_generator.retry_backoff_max_ms);
    let max_retries = settings.demand_generator.max_api_retries;

    loop {
        match operation().await {
            Ok(result) => {
                if retry_count > 0 {
                    info!("{} succeeded after {} retries", operation_name, retry_count);
                }
                return Ok(result);
            }
            Err(e) => {
                retry_count += 1;
                if retry_count > max_retries {
                    return Err(e).context(format!(
                        "{operation_name} failed after {max_retries} retries"
                    ));
                }

                // Check if it's a rate limit error (429)
                let is_rate_limit = e.to_string().contains("429")
                    || e.to_string().to_lowercase().contains("rate limit");

                // Add jitter to prevent thundering herd
                let jitter_factor = {
                    use rand::Rng;
                    let mut rng = rand::thread_rng();
                    0.5 + rng.gen_range(0.0..0.5)
                };
                let jittered_backoff = backoff.mul_f64(jitter_factor);

                let retry_reason = if is_rate_limit { "rate limit" } else { "error" };

                warn!(
                    "{} failed (attempt {}/{}) due to {}: {}. Retrying in {:?}",
                    operation_name, retry_count, max_retries, retry_reason, e, jittered_backoff
                );

                if tracing::enabled!(tracing::Level::DEBUG) {
                    debug!(
                        "Retry details - Base backoff: {:?}, Jittered: {:?}, Rate limit: {}",
                        backoff, jittered_backoff, is_rate_limit
                    );
                }

                sleep(jittered_backoff).await;

                // Increase backoff more aggressively for rate limits
                if is_rate_limit {
                    backoff = backoff
                        .saturating_mul(settings.demand_generator.rate_limit_multiplier)
                        .min(max_backoff);
                } else {
                    backoff = backoff.saturating_mul(2).min(max_backoff);
                }
            }
        }
    }
}

async fn enrich_validators(
    settings: &Settings,
    validators_with_gossip: &[ValidatorDetail],
) -> Result<Vec<EnrichedValidator>> {
    let http_client = Arc::new(reqwest::Client::new());

    // Use configurable concurrent request limit
    let concurrent_limit = settings.demand_generator.concurrent_api_requests as usize;
    let semaphore = Arc::new(Semaphore::new(concurrent_limit));

    // Create a JoinSet to manage concurrent tasks
    let mut set = JoinSet::new();

    info!(
        "Enriching {} validators with max {} concurrent requests",
        validators_with_gossip.len(),
        concurrent_limit
    );

    // Spawn tasks for each validator
    for (index, val_detail) in validators_with_gossip.iter().enumerate() {
        let permit = semaphore.clone().acquire_owned().await?;
        let http_client = http_client.clone();
        let settings = settings.clone();
        let val_detail = val_detail.clone();
        let ip_str = val_detail.ip_address.to_string();

        set.spawn(async move {
            // Keep permit alive for the duration of the task
            let _permit = permit;

            // Wrap the API call with retry logic
            let ip_info_resp = with_retry(
                || async { get_ip_info(&settings, &http_client, &ip_str).await },
                &settings,
                &format!("IP info fetch for {ip_str}"),
            )
            .await?;

            let enriched = EnrichedValidator::new(&val_detail, &ip_info_resp);
            Ok::<(usize, EnrichedValidator), anyhow::Error>((index, enriched))
        });
    }

    // Collect results maintaining original order
    let mut results: Vec<Option<EnrichedValidator>> =
        (0..validators_with_gossip.len()).map(|_| None).collect();
    let mut success_count = 0;
    let mut error_count = 0;
    let start_time = std::time::Instant::now();

    while let Some(join_result) = set.join_next().await {
        match join_result {
            Ok(task_result) => match task_result {
                Ok((index, enriched)) => {
                    results[index] = Some(enriched);
                    success_count += 1;
                }
                Err(e) => {
                    error_count += 1;
                    warn!("Failed to enrich validator: {}", e);
                }
            },
            Err(join_error) => {
                error_count += 1;
                warn!("Task join error: {}", join_error);
            }
        }
    }

    let elapsed = start_time.elapsed();
    info!(
        "API enrichment complete in {:?} - Success: {}/{}, Errors: {}, Rate: {:.2} req/sec",
        elapsed,
        success_count,
        validators_with_gossip.len(),
        error_count,
        success_count as f64 / elapsed.as_secs_f64()
    );

    // Filter out None values and collect successful results
    let enriched: Vec<EnrichedValidator> = results.into_iter().flatten().collect();

    if enriched.is_empty() {
        bail!("Failed to enrich any validators");
    }

    Ok(enriched)
}

fn filter_gossiping_validators(
    ip_map: &ValidatorIpMap,
    vote_accounts: &[RpcVoteAccountInfo],
    stake_map: &HashMap<Pubkey, u64>,
) -> Result<Vec<ValidatorDetail>> {
    let mut seen_validators = HashSet::new();
    let mut all_validators = vec![];

    for vote_account in vote_accounts {
        // Parse the validator's identity pubkey string into a real Pubkey
        if let Ok(identity_pubkey) = vote_account.node_pubkey.parse::<Pubkey>() {
            // Skip if we've already processed this validator
            if !seen_validators.insert(identity_pubkey) {
                continue;
            }

            // Look up the IP for this validator in our HashMap
            if let Some(&ip_address) = ip_map.get(&identity_pubkey) {
                // Get the total stake for this validator
                let stake_lamports = stake_map.get(&identity_pubkey).unwrap_or(&0);

                // If we find an IP, create our final struct and add it to the list
                all_validators.push(ValidatorDetail {
                    identity_pubkey,
                    ip_address,
                    stake_lamports: *stake_lamports,
                });
            }
        }
    }
    Ok(all_validators)
}

fn ip_map(cluster_nodes: Vec<RpcContactInfo>) -> ValidatorIpMap {
    cluster_nodes
        .into_iter()
        .filter_map(|node| {
            // The node.pubkey is a String, so we parse it into a Pubkey.
            // The node.gossip is an Option<SocketAddr>, so we get the ip() from it.
            // If either part fails, this node is ignored.
            if let (Ok(pubkey), Some(gossip_addr)) = (node.pubkey.parse(), node.gossip) {
                Some((pubkey, gossip_addr.ip()))
            } else {
                None
            }
        })
        .collect()
}

async fn get_cluster_nodes(rpc_client: &RpcClient) -> Result<Vec<RpcContactInfo>> {
    let cluster_nodes = rpc_client
        .get_cluster_nodes()
        .await
        .context("Failed to get cluster nodes from the RPC endpoint.")?;
    Ok(cluster_nodes)
}

async fn get_all_validator_identities(rpc_client: &RpcClient) -> Result<Vec<RpcVoteAccountInfo>> {
    let config = RpcGetVoteAccountsConfig {
        keep_unstaked_delinquents: Some(false),
        ..Default::default()
    };
    let vote_accounts = rpc_client
        .get_vote_accounts_with_config(config)
        .await
        .context("Failed to get vote accounts")?;
    let mut all_validators = vote_accounts.current;
    all_validators.extend(vote_accounts.delinquent);
    Ok(all_validators)
}

/// Validates an IP address string
fn validate_ip_address(ip: &str) -> Result<()> {
    use std::{net::IpAddr, str::FromStr};

    IpAddr::from_str(ip).map_err(|_| anyhow::anyhow!("Invalid IP address: {}", ip))?;
    Ok(())
}

async fn get_ip_info(
    settings: &Settings,
    client: &reqwest::Client,
    ip: &str,
) -> Result<IpInfoResp> {
    // Validate IP address format
    validate_ip_address(ip)?;

    let url = format!("{}/{}", settings.demand_generator.ip_info.base_url, ip);

    // Get API token - should always be Some after validation in settings
    let api_token = settings
        .demand_generator
        .ip_info
        .api_token
        .as_ref()
        .ok_or_else(|| anyhow::anyhow!("API token not configured"))?;

    let response = client
        .get(&url)
        .bearer_auth(api_token)
        .send()
        .await?
        .error_for_status()?;
    let ip_info: IpInfoResp = response
        .json()
        .await
        .context("Failed to parse JSON from ipinfo.io response.")?;

    Ok(ip_info)
}
