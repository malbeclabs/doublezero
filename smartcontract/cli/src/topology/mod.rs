pub mod assign_node_segments;
pub mod clear;
pub mod create;
pub mod delete;
pub mod list;

use doublezero_sdk::TopologyInfo;
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;

pub fn resolve_topology_names(
    pubkeys: &[Pubkey],
    topology_map: &HashMap<Pubkey, TopologyInfo>,
) -> String {
    if pubkeys.is_empty() {
        "default".to_string()
    } else {
        pubkeys
            .iter()
            .map(|pk| {
                topology_map
                    .get(pk)
                    .map(|t| t.name.clone())
                    .unwrap_or_else(|| pk.to_string())
            })
            .collect::<Vec<_>>()
            .join(", ")
    }
}
