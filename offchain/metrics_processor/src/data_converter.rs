use crate::{
    data_store::{
        DataStore, Device, Exchange, Link, Location, MulticastGroup, TelemetrySample, User,
    },
    types::RewardsData,
};
use anyhow::Result;

/// Convert fetched RewardsData into DataStore format
pub fn convert_to_datastore(rewards_data: RewardsData) -> Result<DataStore> {
    let mut data_store = DataStore::new(rewards_data.after_us, rewards_data.before_us);

    // Convert locations
    for db_loc in rewards_data.network.locations {
        let location = Location {
            pubkey: db_loc.pubkey.to_string(),
            owner: db_loc.owner.to_string(),
            index: db_loc.index as u64,
            bump_seed: db_loc.bump_seed,
            lat: db_loc.lat,
            lng: db_loc.lng,
            loc_id: db_loc.loc_id,
            status: db_loc.status,
            code: db_loc.code,
            name: db_loc.name,
            country: db_loc.country,
        };
        data_store
            .locations
            .insert(location.pubkey.clone(), location);
    }

    // Convert exchanges
    for db_ex in rewards_data.network.exchanges {
        let exchange = Exchange {
            pubkey: db_ex.pubkey.to_string(),
            owner: db_ex.owner.to_string(),
            index: db_ex.index as u64,
            bump_seed: db_ex.bump_seed,
            lat: db_ex.lat,
            lng: db_ex.lng,
            loc_id: db_ex.loc_id,
            status: db_ex.status,
            code: db_ex.code,
            name: db_ex.name,
        };
        data_store
            .exchanges
            .insert(exchange.pubkey.clone(), exchange);
    }

    // Convert devices
    for db_dev in rewards_data.network.devices {
        let device = Device {
            pubkey: db_dev.pubkey.to_string(),
            owner: db_dev.owner.to_string(),
            index: db_dev.index as u64,
            bump_seed: db_dev.bump_seed,
            location_pubkey: db_dev.location_pubkey.map(|pk| pk.to_string()),
            exchange_pubkey: db_dev.exchange_pubkey.map(|pk| pk.to_string()),
            device_type: db_dev.device_type,
            public_ip: db_dev.public_ip,
            status: db_dev.status,
            code: db_dev.code,
            dz_prefixes: extract_prefixes(&db_dev.dz_prefixes),
            metrics_publisher_pk: db_dev.metrics_publisher_pk.to_string(),
        };
        data_store.devices.insert(device.pubkey.clone(), device);
    }

    // Convert links
    for db_link in rewards_data.network.links {
        let link = Link {
            pubkey: db_link.pubkey.to_string(),
            owner: db_link.owner.to_string(),
            index: db_link.index as u64,
            bump_seed: db_link.bump_seed,
            from_device_pubkey: db_link.from_device_pubkey.map(|pk| pk.to_string()),
            to_device_pubkey: db_link.to_device_pubkey.map(|pk| pk.to_string()),
            link_type: db_link.link_type,
            bandwidth: db_link.bandwidth,
            mtu: db_link.mtu,
            delay_ns: db_link.delay_ns,
            jitter_ns: db_link.jitter_ns,
            tunnel_id: db_link.tunnel_id,
            tunnel_net: extract_tunnel_net(&db_link.tunnel_net),
            status: db_link.status,
            code: db_link.code,
        };
        data_store.links.insert(link.pubkey.clone(), link);
    }

    // Convert users
    for db_user in rewards_data.network.users {
        let user = User {
            pubkey: db_user.pubkey.to_string(),
            owner: db_user.owner.to_string(),
            index: db_user.index as u64,
            bump_seed: db_user.bump_seed,
            user_type: db_user.user_type,
            tenant_pk: db_user.tenant_pk.to_string(),
            device_pk: db_user.device_pk.map(|pk| pk.to_string()),
            cyoa_type: db_user.cyoa_type,
            client_ip: db_user.client_ip,
            dz_ip: db_user.dz_ip,
            tunnel_id: db_user.tunnel_id,
            tunnel_net: extract_tunnel_net(&db_user.tunnel_net),
            status: db_user.status,
            publishers: db_user.publishers.iter().map(|pk| pk.to_string()).collect(),
            subscribers: db_user
                .subscribers
                .iter()
                .map(|pk| pk.to_string())
                .collect(),
        };
        data_store.users.insert(user.pubkey.clone(), user);
    }

    // Convert multicast groups
    for db_group in rewards_data.network.multicast_groups {
        let group = MulticastGroup {
            pubkey: db_group.pubkey.to_string(),
            owner: db_group.owner.to_string(),
            index: db_group.index as u64,
            bump_seed: db_group.bump_seed,
            tenant_pk: db_group.tenant_pk.to_string(),
            multicast_ip: db_group.multicast_ip,
            max_bandwidth: db_group.max_bandwidth,
            status: db_group.status,
            code: db_group.code,
            pub_allowlist: db_group
                .pub_allowlist
                .iter()
                .map(|pk| pk.to_string())
                .collect(),
            sub_allowlist: db_group
                .sub_allowlist
                .iter()
                .map(|pk| pk.to_string())
                .collect(),
            publishers: db_group
                .publishers
                .iter()
                .map(|pk| pk.to_string())
                .collect(),
            subscribers: db_group
                .subscribers
                .iter()
                .map(|pk| pk.to_string())
                .collect(),
        };
        data_store
            .multicast_groups
            .insert(group.pubkey.clone(), group);
    }

    // Convert telemetry samples
    for db_sample in rewards_data.telemetry.device_latency_samples {
        let sample = TelemetrySample {
            pubkey: db_sample.pubkey.to_string(),
            epoch: db_sample.epoch,
            origin_device_pk: db_sample.origin_device_pk.to_string(),
            target_device_pk: db_sample.target_device_pk.to_string(),
            link_pk: db_sample.link_pk.to_string(),
            origin_device_location_pk: db_sample.origin_device_location_pk.to_string(),
            target_device_location_pk: db_sample.target_device_location_pk.to_string(),
            origin_device_agent_pk: db_sample.origin_device_agent_pk.to_string(),
            sampling_interval_us: db_sample.sampling_interval_us,
            start_timestamp_us: db_sample.start_timestamp_us,
            samples: db_sample.samples,
            sample_count: db_sample.sample_count,
        };
        data_store.telemetry_samples.push(sample);
    }

    // TODO: Load internet baselines and demand matrix from external sources

    Ok(data_store)
}

/// Extract IP prefixes from JSON value
fn extract_prefixes(json: &serde_json::Value) -> Vec<String> {
    if let Some(arr) = json.as_array() {
        arr.iter()
            .filter_map(|v| {
                if let (Some(ip), Some(prefix)) = (v.get("ip"), v.get("prefix")) {
                    Some(format!("{}/{}", ip.as_str()?, prefix.as_u64()?))
                } else {
                    None
                }
            })
            .collect()
    } else {
        vec![]
    }
}

/// Extract tunnel network from JSON value
fn extract_tunnel_net(json: &serde_json::Value) -> Vec<String> {
    if let (Some(ip), Some(prefix)) = (json.get("ip"), json.get("prefix")) {
        vec![format!(
            "{}/{}",
            ip.as_str().unwrap_or(""),
            prefix.as_u64().unwrap_or(0)
        )]
    } else {
        vec![]
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_extract_prefixes() {
        let json = json!([
            {"ip": "10.0.0.0", "prefix": 24},
            {"ip": "192.168.1.0", "prefix": 16}
        ]);

        let prefixes = extract_prefixes(&json);
        assert_eq!(prefixes, vec!["10.0.0.0/24", "192.168.1.0/16"]);
    }

    #[test]
    fn test_extract_tunnel_net() {
        let json = json!({"ip": "10.0.0.0", "prefix": 24});
        let tunnel_net = extract_tunnel_net(&json);
        assert_eq!(tunnel_net, vec!["10.0.0.0/24"]);
    }
}
