use crate::{
    city_aggregator::aggregate_by_city,
    constants::SOLANA_MAINNET_RPC_URL,
    demand_matrix::{DemandConfig, generate_demand_matrix},
    settings::Settings,
    types::{EnrichedValidator, IpInfoResp, ValidatorDetail, ValidatorIpMap},
};
use anyhow::{Context, Result};
use network_shapley::types::Demand;
use solana_client::{
    nonblocking::rpc_client::RpcClient,
    rpc_config::RpcGetVoteAccountsConfig,
    rpc_response::{RpcContactInfo, RpcVoteAccountInfo},
};
use solana_sdk::pubkey::Pubkey;
use std::collections::{HashMap, HashSet};
use tracing::info;

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
        let rpc_client = RpcClient::new(SOLANA_MAINNET_RPC_URL.to_string());
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
        info!("unique validator identities: {}", unique_validator_count);

        // Get all cluster nodes
        let cluster_nodes = get_cluster_nodes(&rpc_client).await?;

        // Convert the list of nodes into a map for lookups
        // A single validator can have multiple vote accounts. We only want one entry per validator identity.
        let ip_map = ip_map(cluster_nodes);

        // Combine to construct Vec<ValidatorDetails>
        let validators_with_gossip =
            filter_gossiping_validators(&ip_map, &vote_accounts, &stake_map)?;
        info!("gossiping validators: {:?}", validators_with_gossip.len());

        // Add ip info data
        let enriched_validators =
            enrich_validators(&self.settings, &validators_with_gossip).await?;
        info!("enriched validators: {:?}", enriched_validators.len());

        // Aggregate validators by city
        let city_aggregates = aggregate_by_city(&enriched_validators)?;
        info!("aggregated into {} cities", city_aggregates.len());

        // Generate demand matrix
        let config = DemandConfig::default();
        let demands = generate_demand_matrix(&city_aggregates, &config)?;
        info!("generated {} demand entries", demands.len());

        Ok((enriched_validators, demands))
    }
}

async fn enrich_validators(
    settings: &Settings,
    validators_with_gossip: &[ValidatorDetail],
) -> Result<Vec<EnrichedValidator>> {
    let http_client = reqwest::Client::new();
    let mut enriched = vec![];
    for val_detail in validators_with_gossip.iter() {
        let ip_info_resp =
            get_ip_info(settings, &http_client, &val_detail.ip_address.to_string()).await?;

        let val = EnrichedValidator::new(val_detail, &ip_info_resp);
        enriched.push(val)
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

async fn get_ip_info(
    settings: &Settings,
    client: &reqwest::Client,
    ip: &str,
) -> Result<IpInfoResp> {
    let url = format!(
        "{}/{}?token={}",
        settings.demand_generator.ip_info.base_url, ip, settings.demand_generator.ip_info.api_token
    );
    let response = client.get(&url).send().await?.error_for_status()?;
    let ip_info: IpInfoResp = response
        .json()
        .await
        .context("Failed to parse JSON from ipinfo.io response.")?;

    Ok(ip_info)
}
