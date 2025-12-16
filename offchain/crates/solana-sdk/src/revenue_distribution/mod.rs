pub mod compute_unit;

//

use anyhow::{Context, Result};

/// First DZ epoch to generate rewards for network contributors.
pub const GENESIS_DZ_EPOCH_MAINNET_BETA: u64 = 31;

pub use doublezero_revenue_distribution::{ID, env, instruction, state, types};

pub fn try_is_processed_leaf(processed_leaf_data: &[u8], leaf_index: usize) -> Result<bool> {
    // Calculate which byte contains the bit for this leaf index
    // (8 bits per byte, so divide by 8).
    let leaf_byte_index = leaf_index / 8;

    // First, we have to grab the relevant byte from the processed data.
    // Create ByteFlags from the byte value to check the bit.
    let leaf_byte = processed_leaf_data
        .get(leaf_byte_index)
        .copied()
        .map(doublezero_revenue_distribution::types::ByteFlags::new)
        .with_context(|| format!("Invalid leaf index: {leaf_index}"))?;

    // Calculate which bit within the byte corresponds to this leaf
    // (modulo 8 gives us the bit position within the byte: 0-7).
    Ok(leaf_byte.bit(leaf_index % 8))
}
