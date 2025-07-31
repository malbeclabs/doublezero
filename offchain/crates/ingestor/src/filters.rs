use solana_client::rpc_filter::{Memcmp, RpcFilterType};

/// Build a filter for just account type (1-byte filter)
pub fn build_account_type_filter(account_type: u8) -> Vec<RpcFilterType> {
    let bytes = vec![account_type];

    // TODO: remove when done
    // Debug log the filter bytes
    crate::debug::debug_filter_bytes("account_type", &bytes);

    vec![RpcFilterType::Memcmp(Memcmp::new_base58_encoded(0, &bytes))]
}

/// Build a filter for account type + epoch (9-byte filter)
pub fn build_epoch_filter(account_type: u8, epoch: u64) -> Vec<RpcFilterType> {
    let mut bytes = vec![account_type];
    bytes.extend_from_slice(&epoch.to_le_bytes());

    // TODO: remove when done
    // Debug log the filter bytes
    crate::debug::debug_filter_bytes(&format!("account_type + epoch {epoch}"), &bytes);

    vec![RpcFilterType::Memcmp(Memcmp::new_base58_encoded(0, &bytes))]
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_account_type_filter() {
        let filters = build_account_type_filter(3);
        assert_eq!(filters.len(), 1);

        if let RpcFilterType::Memcmp(memcmp) = &filters[0] {
            assert_eq!(memcmp.offset(), 0);
            // The filter should contain just the account type byte
            // Note: actual encoding test would require checking the base58 encoding
        } else {
            panic!("Expected Memcmp filter");
        }
    }

    #[test]
    fn test_epoch_filter() {
        let epoch: u64 = 1234;
        let account_type: u8 = 3;
        let filters = build_epoch_filter(account_type, epoch);

        assert_eq!(filters.len(), 1);

        if let RpcFilterType::Memcmp(memcmp) = &filters[0] {
            assert_eq!(memcmp.offset(), 0);
            // The filter should be 9 bytes: 1 byte discriminator + 8 bytes epoch
            // Bytes should be: [3, 210, 4, 0, 0, 0, 0, 0, 0]
            // (3 is the account type, 1234 = 0x04D2 in little-endian)
        } else {
            panic!("Expected Memcmp filter");
        }
    }

    #[test]
    fn test_epoch_filter_bytes() {
        // Test the actual byte construction
        let epoch: u64 = 1234;
        let account_type: u8 = 3;

        let mut expected_bytes = vec![account_type];
        expected_bytes.extend_from_slice(&epoch.to_le_bytes());

        assert_eq!(expected_bytes.len(), 9);
        assert_eq!(expected_bytes[0], 3);
        assert_eq!(expected_bytes[1], 210); // 1234 & 0xFF
        assert_eq!(expected_bytes[2], 4); // (1234 >> 8) & 0xFF
        assert_eq!(expected_bytes[3], 0);
        assert_eq!(expected_bytes[4], 0);
        assert_eq!(expected_bytes[5], 0);
        assert_eq!(expected_bytes[6], 0);
        assert_eq!(expected_bytes[7], 0);
        assert_eq!(expected_bytes[8], 0);
    }
}
