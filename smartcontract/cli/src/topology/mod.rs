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
    resolve_topology_names_with(pubkeys, topology_map, |pk| pk.to_string())
}

/// Like [`resolve_topology_names`] but abbreviates the pubkey shown for an
/// unknown topology (one not in `topology_map`), for narrow output. Known
/// topology names are kept in full.
pub fn resolve_topology_names_short(
    pubkeys: &[Pubkey],
    topology_map: &HashMap<Pubkey, TopologyInfo>,
) -> String {
    resolve_topology_names_with(pubkeys, topology_map, crate::util::display_pubkey_short)
}

fn resolve_topology_names_with(
    pubkeys: &[Pubkey],
    topology_map: &HashMap<Pubkey, TopologyInfo>,
    fallback: impl Fn(&Pubkey) -> String,
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
                    .unwrap_or_else(|| fallback(pk))
            })
            .collect::<Vec<_>>()
            .join(", ")
    }
}

#[cfg(test)]
mod tests {
    use super::{resolve_topology_names, resolve_topology_names_short};
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    #[test]
    fn short_resolver_abbreviates_unknown_topology() {
        let map = HashMap::new();
        let pk = Pubkey::new_unique();
        // Unknown topology (not in the map) → shortened pubkey for narrow output.
        assert_eq!(
            resolve_topology_names_short(&[pk], &map),
            crate::util::display_pubkey_short(&pk)
        );
        // The full resolver keeps the unabbreviated 44-char key.
        assert_eq!(resolve_topology_names(&[pk], &map), pk.to_string());
        // Empty → "default" for both.
        assert_eq!(resolve_topology_names_short(&[], &map), "default");
    }
}
