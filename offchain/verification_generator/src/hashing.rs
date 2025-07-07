//! Deterministic hashing utilities for verification packets
//!
//! This module provides functions for creating deterministic hashes of data structures
//! to ensure reproducibility and verifiability of reward calculations.

use anyhow::{Context, Result};
use serde::Serialize;
use sha2::{Digest, Sha256};

/// Hash a serializable data structure to a hex string
///
/// This function:
/// 1. Serializes the input data to canonical JSON bytes using serde_json::to_vec
/// 2. Computes the SHA-256 hash of the resulting bytes
/// 3. Encodes the hash digest as a lowercase hexadecimal string
///
/// # Arguments
/// * `data` - Any data structure that implements Serialize
///
/// # Returns
/// A hex-encoded SHA-256 hash string
pub fn hash_serializable<T: Serialize>(data: &T) -> Result<String> {
    // TODO: Investigate more compact serialization crates? Borsh for solana compat maybe? Does not
    // have to be borsh though specifically.
    // Serialize to canonical JSON bytes
    let json_bytes = serde_json::to_vec(data).context("Failed to serialize data for hashing")?;

    // Compute SHA-256 hash
    let mut hasher = Sha256::new();
    hasher.update(&json_bytes);
    let hash_bytes = hasher.finalize();

    // Convert to hex string
    Ok(hex::encode(hash_bytes))
}

/// Hash raw bytes to a hex string
pub fn hash_bytes(data: &[u8]) -> String {
    let mut hasher = Sha256::new();
    hasher.update(data);
    let hash_bytes = hasher.finalize();
    hex::encode(hash_bytes)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde::Serialize;
    use std::collections::BTreeMap;

    #[derive(Serialize)]
    struct TestData {
        value: u64,
        name: String,
    }

    #[test]
    fn test_hash_deterministic() {
        let data1 = TestData {
            value: 42,
            name: "test".to_string(),
        };

        let data2 = TestData {
            value: 42,
            name: "test".to_string(),
        };

        let hash1 = hash_serializable(&data1).unwrap();
        let hash2 = hash_serializable(&data2).unwrap();

        // Same data should produce same hash
        assert_eq!(hash1, hash2);

        // Hash should be 64 characters (32 bytes in hex)
        assert_eq!(hash1.len(), 64);
    }

    #[test]
    fn test_hash_different_data() {
        let data1 = TestData {
            value: 42,
            name: "test".to_string(),
        };

        let data2 = TestData {
            value: 43,
            name: "test".to_string(),
        };

        let hash1 = hash_serializable(&data1).unwrap();
        let hash2 = hash_serializable(&data2).unwrap();

        // Different data should produce different hashes
        assert_ne!(hash1, hash2);
    }

    #[test]
    fn test_hash_btreemap_deterministic() {
        // BTreeMap should serialize keys in sorted order
        let mut map1 = BTreeMap::new();
        map1.insert("b", 2);
        map1.insert("a", 1);
        map1.insert("c", 3);

        let mut map2 = BTreeMap::new();
        map2.insert("c", 3);
        map2.insert("a", 1);
        map2.insert("b", 2);

        let hash1 = hash_serializable(&map1).unwrap();
        let hash2 = hash_serializable(&map2).unwrap();

        // Same data in different insertion order should produce same hash
        assert_eq!(hash1, hash2);
    }

    #[test]
    fn test_hash_known_value() {
        #[derive(Serialize)]
        struct SimpleData {
            value: u64,
        }

        let data = SimpleData { value: 42 };
        let hash = hash_serializable(&data).unwrap();

        // Verify the hash is stable (this is the SHA-256 of {"value":42})
        assert_eq!(
            hash,
            "dc60e632a90329ccfd34fbe904d94704dbbb6669575185e26389854ff64139c3"
        );
    }

    #[test]
    fn test_hash_empty_data() {
        // Test with empty struct
        #[derive(Serialize)]
        struct EmptyData {}

        let data = EmptyData {};
        let hash = hash_serializable(&data).unwrap();

        // Empty object {} should still produce a valid hash
        assert!(!hash.is_empty());
        assert_eq!(hash.len(), 64);
    }

    #[test]
    fn test_hash_empty_collections() {
        // Empty vec
        let empty_vec: Vec<i32> = vec![];
        let hash1 = hash_serializable(&empty_vec).unwrap();
        assert_eq!(hash1.len(), 64);

        // Empty BTreeMap
        let empty_map: BTreeMap<String, i32> = BTreeMap::new();
        let hash2 = hash_serializable(&empty_map).unwrap();
        assert_eq!(hash2.len(), 64);

        // They should produce different hashes
        assert_ne!(hash1, hash2);
    }

    #[test]
    fn test_hash_large_data() {
        // Test with large data structure
        let mut large_map = BTreeMap::new();
        for i in 0..1000 {
            large_map.insert(format!("key_{i}"), i);
        }

        let hash = hash_serializable(&large_map).unwrap();
        assert_eq!(hash.len(), 64);

        // Verify it's deterministic even with large data
        let hash2 = hash_serializable(&large_map).unwrap();
        assert_eq!(hash, hash2);
    }

    #[test]
    fn test_hash_nested_structures() {
        #[derive(Serialize)]
        struct Inner {
            value: i32,
        }

        #[derive(Serialize)]
        struct Outer {
            name: String,
            inner: Inner,
            list: Vec<i32>,
        }

        let data = Outer {
            name: "test".to_string(),
            inner: Inner { value: 42 },
            list: vec![1, 2, 3],
        };

        let hash1 = hash_serializable(&data).unwrap();
        let hash2 = hash_serializable(&data).unwrap();
        assert_eq!(hash1, hash2);
    }

    #[test]
    fn test_hash_special_characters() {
        use serde_json::json;

        // Test with special characters and unicode
        let data = json!({
            "emoji": "🚀",
            "special": "!@#$%^&*()",
            "unicode": "你好世界",
            "escaped": "\"quotes\" and \\backslashes\\"
        });

        let hash = hash_serializable(&data).unwrap();
        assert_eq!(hash.len(), 64);

        // Should be deterministic
        let hash2 = hash_serializable(&data).unwrap();
        assert_eq!(hash, hash2);
    }

    #[test]
    fn test_hash_numeric_types() {
        // Test that different numeric values produce different hashes
        // Note: JSON serialization doesn't preserve type information,
        // so 42u32 and 42i32 both serialize to 42
        assert_ne!(
            hash_serializable(&42u32).unwrap(),
            hash_serializable(&43u32).unwrap()
        );
        assert_ne!(
            hash_serializable(&42i32).unwrap(),
            hash_serializable(&-42i32).unwrap()
        );
        assert_ne!(
            hash_serializable(&42.0f32).unwrap(),
            hash_serializable(&42.1f32).unwrap()
        );

        // Verify that same numeric value with same type produces same hash
        assert_eq!(
            hash_serializable(&42u32).unwrap(),
            hash_serializable(&42u32).unwrap()
        );

        // Document that different types with same value produce same hash due to JSON
        assert_eq!(
            hash_serializable(&42i32).unwrap(),
            hash_serializable(&42u32).unwrap(),
            "JSON serialization converts both to the number 42"
        );
        assert_eq!(
            hash_serializable(&42u8).unwrap(),
            hash_serializable(&42u16).unwrap(),
            "JSON serialization converts both to the number 42"
        );
    }

    #[test]
    fn test_hash_bytes_edge_cases() {
        // Test direct bytes hashing
        let empty_bytes: &[u8] = &[];
        let hash1 = hash_bytes(empty_bytes);
        assert_eq!(hash1.len(), 64);

        let single_byte: &[u8] = &[0x42];
        let hash2 = hash_bytes(single_byte);
        assert_eq!(hash2.len(), 64);
        assert_ne!(hash1, hash2);

        let large_bytes: Vec<u8> = (0..1024).map(|i| (i % 256) as u8).collect();
        let hash3 = hash_bytes(&large_bytes);
        assert_eq!(hash3.len(), 64);
    }

    #[test]
    fn test_hash_option_types() {
        #[derive(Serialize)]
        struct OptionalData {
            required: String,
            optional: Option<i32>,
        }

        let data_some = OptionalData {
            required: "test".to_string(),
            optional: Some(42),
        };

        let data_none = OptionalData {
            required: "test".to_string(),
            optional: None,
        };

        let hash_some = hash_serializable(&data_some).unwrap();
        let hash_none = hash_serializable(&data_none).unwrap();

        // Different Option values should produce different hashes
        assert_ne!(hash_some, hash_none);
    }
}
