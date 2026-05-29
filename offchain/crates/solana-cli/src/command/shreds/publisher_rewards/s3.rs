// VENDORED from `malbeclabs/doublezero-shreds`:
// `crates/shred-oracle/src/validator_rewards/s3.rs`. Kept in sync by hand
// because offchain only needs the S3 fetch + merkle-tree primitives, not the
// rest of the oracle. Remove this file once the shreds repo is merged into
// the monorepo and we can depend on `doublezero_shred_oracle::validator_rewards::s3`
// directly.

use std::{str::FromStr, time::Duration};

use anyhow::{Context, Result, ensure};
use doublezero_solana_sdk::{
    Pubkey,
    merkle::{MerkleProof, merkle_root_from_indexed_pod_leaves},
    sha2::Hash,
    shred_subscription::types::ValidatorRewardsLeaf,
};
use reqwest::Client;
use serde::Deserialize;
use tracing::{debug, warn};

pub const S3_BASE_URL: &str = "https://doublezero-foundation-public.s3.us-east-2.amazonaws.com/exports/multicast_validator_leader_slots";

pub fn build_s3_client() -> Result<Client> {
    Client::builder()
        .connect_timeout(Duration::from_secs(10))
        .timeout(Duration::from_secs(30))
        .build()
        .context("build S3 reqwest client")
}

#[derive(Debug, Clone, Deserialize)]
#[allow(dead_code)] // `epoch` is in the wire format but unused offchain (URL carries it).
pub struct ValidatorLeaderSlotEntry {
    pub epoch: u64,
    pub node_identity: String,
    pub client_id: u16,
    pub number_of_leader_slots: u32,
}

#[derive(Debug, Clone)]
#[allow(dead_code)] // `root` + totals are kept for parity with the canonical impl; not used offchain today.
pub struct ComputedLeaves {
    pub leaves: Vec<ValidatorRewardsLeaf>,
    pub root: Hash,
    pub total_publishing_validators: u32,
    pub total_published_leader_slots: u32,
}

pub async fn fetch_leader_slot_data(
    client: &Client,
    solana_epoch: u64,
) -> Result<Vec<ValidatorLeaderSlotEntry>> {
    let url = format!("{S3_BASE_URL}/{solana_epoch}.json");
    debug!(url, "Fetching validator leader-slot data");

    let response = client
        .get(&url)
        .send()
        .await
        .with_context(|| format!("HTTP request to {url}"))?;

    ensure!(
        response.status().is_success(),
        "S3 returned status {} for epoch {solana_epoch}",
        response.status(),
    );

    let entries = response
        .json::<Vec<ValidatorLeaderSlotEntry>>()
        .await
        .with_context(|| format!("deserialize leader-slot JSON for epoch {solana_epoch}"))?;

    debug!(count = entries.len(), "Fetched validator entries");
    Ok(entries)
}

pub fn compute_leaves(entries: &[ValidatorLeaderSlotEntry]) -> Result<ComputedLeaves> {
    ensure!(!entries.is_empty(), "no validator entries to compute root");

    let mut leaves = entries
        .iter()
        .filter_map(|entry| match Pubkey::from_str(&entry.node_identity) {
            Ok(pubkey) => Some(ValidatorRewardsLeaf::new(
                pubkey,
                entry.number_of_leader_slots,
                entry.client_id,
            )),
            Err(err) => {
                warn!(
                    node_identity = %entry.node_identity,
                    client_id = entry.client_id,
                    %err,
                    "dropping entry with unparseable node_identity \
                     (must match canonical oracle; if this is a real \
                     validator the merkle root will diverge from on-chain)"
                );
                None
            }
        })
        .collect::<Vec<_>>();

    leaves.sort_unstable_by_key(|l| (l.node_id, l.client_id));

    if let Some(pair) = leaves
        .windows(2)
        .find(|w| w[0].node_id == w[1].node_id && w[0].client_id == w[1].client_id)
    {
        anyhow::bail!(
            "duplicate (node_id, client_id) pair: node_id {}, client_id {} in validator leader-slot data",
            pair[0].node_id,
            pair[0].client_id,
        );
    }

    let total = <u32>::try_from(leaves.len()).context("too many validators")?;
    ensure!(total > 0, "no valid validator entries after filtering");

    let total_published_leader_slots = leaves.iter().map(|leaf| leaf.leader_slots).try_fold(
        u32::default(),
        |running_total, slots| {
            running_total
                .checked_add(slots)
                .context("total published leader slots overflow")
        },
    )?;

    let root =
        merkle_root_from_indexed_pod_leaves(&leaves, Some(ValidatorRewardsLeaf::LEAF_PREFIX))
            .context("failed to compute merkle root")?;

    Ok(ComputedLeaves {
        leaves,
        root,
        total_publishing_validators: total,
        total_published_leader_slots,
    })
}

// ---------------------------------------------------------------------------
// OFFCHAIN-ONLY ADDITIONS (not in canonical doublezero-shreds).
// These helpers layer on top of the vendored primitives above for offchain
// CLI use. When the shreds repo merges into the monorepo, decide whether
// to upstream these or fold them back into the caller.
// ---------------------------------------------------------------------------

/// Compute the merkle proof for a single leaf at `leaf_index` against the
/// sorted leaf set returned by [`compute_leaves`]. Per-leaf cost is
/// O(log N), so callers that only need a few proofs out of a tree pay
/// proportionally — for the post-configure distribute pass at ~1500
/// validators × 100 epochs of lookback, that's ~10 ops per matched leaf
/// instead of the ~16k ops per epoch a build-every-proof approach would
/// cost.
pub fn compute_proof_for_leaf(
    leaves: &[ValidatorRewardsLeaf],
    leaf_index: usize,
) -> Result<MerkleProof> {
    let leaf_index_u32 =
        u32::try_from(leaf_index).context("leaf_index too large for merkle proof")?;
    MerkleProof::from_indexed_pod_leaves(
        leaves,
        leaf_index_u32,
        Some(ValidatorRewardsLeaf::LEAF_PREFIX),
    )
    .with_context(|| format!("compute merkle proof for leaf {leaf_index}"))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_entry(identity: &str, client_id: u16, slots: u32) -> ValidatorLeaderSlotEntry {
        ValidatorLeaderSlotEntry {
            epoch: 951,
            node_identity: identity.to_string(),
            client_id,
            number_of_leader_slots: slots,
        }
    }

    /// PARITY PIN: canonical oracle's `compute_leaves` silently drops
    /// entries whose `node_identity` doesn't parse as a Pubkey. This
    /// test pins the same behavior in our offchain mirror — if a future
    /// PR changes `compute_leaves` to hard-fail on bad parses, this
    /// test fails and forces the author to also update the canonical
    /// (or document why the divergence is acceptable).
    #[test]
    fn compute_leaves_drops_unparseable_pubkeys_silently() {
        let valid_pubkey = Pubkey::new_unique();
        let entries = vec![
            make_entry(&valid_pubkey.to_string(), 1, 100),
            // Two clearly unparseable entries — different malformations
            // to exercise both lengths/charsets.
            make_entry("not-a-pubkey", 2, 200),
            make_entry("", 3, 300),
        ];
        let computed =
            compute_leaves(&entries).expect("valid entry survives; bad entries are dropped");
        assert_eq!(
            computed.leaves.len(),
            1,
            "only the valid entry should remain"
        );
        assert_eq!(computed.leaves[0].node_id, valid_pubkey);
        assert_eq!(computed.total_publishing_validators, 1);
        assert_eq!(computed.total_published_leader_slots, 100);
    }

    /// End-to-end check of the offchain proof path: build the sorted
    /// leaf set, then assert each per-leaf proof reconstructs the same
    /// root that `compute_leaves` produced. This is the contract that
    /// matters on-chain — the distribute ix verifies leaves against the
    /// posted root via these proofs.
    #[test]
    fn compute_proof_for_leaf_reconstructs_root() {
        let pk1 = Pubkey::new_unique();
        let pk2 = Pubkey::new_unique();
        let pk3 = Pubkey::new_unique();
        let entries = vec![
            make_entry(&pk2.to_string(), 2, 200),
            make_entry(&pk1.to_string(), 1, 100),
            make_entry(&pk3.to_string(), 3, 300),
        ];

        let computed = compute_leaves(&entries).unwrap();

        assert_eq!(computed.leaves.len(), 3);
        assert_eq!(computed.total_publishing_validators, 3);
        assert_eq!(computed.total_published_leader_slots, 600);
        // Sorted by (node_id, client_id) — secondary key is irrelevant
        // here since all three node_ids differ.
        assert!(computed.leaves[0].node_id < computed.leaves[1].node_id);
        assert!(computed.leaves[1].node_id < computed.leaves[2].node_id);

        for (leaf_index, leaf) in computed.leaves.iter().enumerate() {
            let proof = compute_proof_for_leaf(&computed.leaves, leaf_index).unwrap();
            let reconstructed =
                proof.root_from_pod_leaf(leaf, Some(ValidatorRewardsLeaf::LEAF_PREFIX));
            assert_eq!(reconstructed, computed.root, "leaf_index {leaf_index}");
        }
    }
}
