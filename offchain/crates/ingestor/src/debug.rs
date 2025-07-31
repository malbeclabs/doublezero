// TODO: Remove me

use tracing::{debug, warn};

/// Hex dump the first N bytes of account data for debugging
pub fn hex_dump_account_prefix(account_data: &[u8], len: usize) {
    let to_dump = account_data.len().min(len);
    debug!(
        "Account data prefix ({} bytes): {:02x?}",
        to_dump,
        &account_data[..to_dump]
    );
}

/// Verify the epoch field is at the expected offset (1) and extract its value
pub fn verify_epoch_offset(account_data: &[u8]) -> Option<u64> {
    if account_data.len() >= 9 {
        let epoch_bytes = &account_data[1..9];
        let epoch = u64::from_le_bytes(epoch_bytes.try_into().ok()?);
        debug!("Epoch at offset 1: {}", epoch);
        Some(epoch)
    } else {
        None
    }
}

/// Debug helper to log detailed account information
pub fn debug_account_structure(pubkey: &str, account_data: &[u8], expected_epoch: Option<u64>) {
    if account_data.is_empty() {
        warn!("Account {} has empty data", pubkey);
        return;
    }

    debug!("=== Account Debug Info ===");
    debug!("Pubkey: {}", pubkey);
    debug!("Total size: {} bytes", account_data.len());

    if !account_data.is_empty() {
        debug!("Discriminator (byte 0): {}", account_data[0]);
    }

    // Hex dump first 32 bytes
    hex_dump_account_prefix(account_data, 32);

    // Try to extract epoch
    if let Some(epoch) = verify_epoch_offset(account_data) {
        debug!("Found epoch: {}", epoch);
        if let Some(expected) = expected_epoch {
            if epoch != expected {
                warn!(
                    "Account {} has epoch {} but filter was for epoch {}",
                    pubkey, epoch, expected
                );
            }
        }
    } else {
        warn!("Could not extract epoch from account {}", pubkey);
    }

    debug!("=========================");
}

/// Log the filter bytes being used for RPC queries
pub fn debug_filter_bytes(filter_type: &str, bytes: &[u8]) {
    debug!(
        "RPC filter for {}: {} bytes = {:02x?}",
        filter_type,
        bytes.len(),
        bytes
    );
}
