use crate::types::{
    parse_utils::{bandwidth_parse, bandwidth_to_string},
    NetworkV4List,
};
use serde::{Deserialize, Deserializer, Serialize, Serializer};
use solana_program::pubkey::Pubkey;
use std::{collections::BTreeMap, str::FromStr};

// ----------------------------------------------
// Serializers
// ----------------------------------------------

pub fn serialize_pubkey_as_string<S>(pubkey: &Pubkey, serializer: S) -> Result<S::Ok, S::Error>
where
    S: Serializer,
{
    serializer.serialize_str(&pubkey.to_string())
}

pub fn serialize_pubkeylist_as_string<S>(
    pubkey: &[Pubkey],
    serializer: S,
) -> Result<S::Ok, S::Error>
where
    S: Serializer,
{
    serializer.serialize_str(
        &pubkey
            .iter()
            .map(|p| p.to_string())
            .collect::<Vec<_>>()
            .join(", "),
    )
}

pub fn serialize_bandwidth_as_string<S>(bandwidth: &u64, serializer: S) -> Result<S::Ok, S::Error>
where
    S: serde::Serializer,
{
    serializer.serialize_str(&bandwidth_to_string(bandwidth))
}

pub fn serialize_networkv4list_as_string<S>(
    list: &NetworkV4List,
    serializer: S,
) -> Result<S::Ok, S::Error>
where
    S: Serializer,
{
    serializer.serialize_str(list.to_string().as_str())
}

// ----------------------------------------------
// Deserializers
// ----------------------------------------------

pub fn deserialize_pubkey_from_string<'de, D>(deserializer: D) -> Result<Pubkey, D::Error>
where
    D: Deserializer<'de>,
{
    let s = String::deserialize(deserializer)?;
    Pubkey::from_str(&s).map_err(serde::de::Error::custom)
}

pub fn deserialize_pubkeylist_from_string<'de, D>(deserializer: D) -> Result<Vec<Pubkey>, D::Error>
where
    D: Deserializer<'de>,
{
    let s = String::deserialize(deserializer)?;
    if s.is_empty() {
        return Ok(Vec::new());
    }

    s.split(',')
        .map(|pubkey_str| {
            let trimmed = pubkey_str.trim();
            Pubkey::from_str(trimmed)
                .map_err(|e| serde::de::Error::custom(format!("Invalid pubkey '{trimmed}': {e}")))
        })
        .collect()
}

pub fn deserialize_networkv4list_from_string<'de, D>(
    deserializer: D,
) -> Result<NetworkV4List, D::Error>
where
    D: Deserializer<'de>,
{
    let s = String::deserialize(deserializer)?;
    NetworkV4List::from_str(&s).map_err(serde::de::Error::custom)
}

pub fn deserialize_bandwidth_from_string<'de, D>(deserializer: D) -> Result<u64, D::Error>
where
    D: Deserializer<'de>,
{
    let s = String::deserialize(deserializer)?;
    bandwidth_parse(&s).map_err(serde::de::Error::custom)
}

// ----------------------------------------------
// Custom serialization where Pubkey is a key in a HashMap
// ----------------------------------------------

pub fn serialize_pubkey_map<S, T>(
    map: &std::collections::HashMap<Pubkey, T>,
    serializer: S,
) -> Result<S::Ok, S::Error>
where
    S: Serializer,
    T: Serialize,
{
    use serde::ser::SerializeMap;

    let mut map_serializer = serializer.serialize_map(Some(map.len()))?;
    for (k, v) in map {
        map_serializer.serialize_entry(&k.to_string(), v)?;
    }
    map_serializer.end()
}

pub fn deserialize_pubkey_map<'de, D, T>(
    deserializer: D,
) -> Result<std::collections::HashMap<Pubkey, T>, D::Error>
where
    D: Deserializer<'de>,
    T: Deserialize<'de>,
{
    let string_map: std::collections::HashMap<String, T> =
        std::collections::HashMap::deserialize(deserializer)?;

    string_map
        .into_iter()
        .map(|(k, v)| {
            Pubkey::from_str(&k)
                .map(|pubkey| (pubkey, v))
                .map_err(|e| serde::de::Error::custom(format!("Invalid pubkey key '{k}': {e}")))
        })
        .collect()
}

// ----------------------------------------------
// Custom serialization where Pubkey is a key in a BTreeMap
// ----------------------------------------------

/// Serialize a BTreeMap with Pubkey as keys
pub fn serialize_pubkey_btreemap<S, T>(
    map: &BTreeMap<Pubkey, T>,
    serializer: S,
) -> Result<S::Ok, S::Error>
where
    S: Serializer,
    T: Serialize,
{
    let string_map: BTreeMap<String, &T> = map.iter().map(|(k, v)| (k.to_string(), v)).collect();
    string_map.serialize(serializer)
}

/// Deserialize a BTreeMap with Pubkey as keys
pub fn deserialize_pubkey_btreemap<'de, D, T>(
    deserializer: D,
) -> Result<BTreeMap<Pubkey, T>, D::Error>
where
    D: Deserializer<'de>,
    T: Deserialize<'de>,
{
    let string_map: BTreeMap<String, T> = BTreeMap::deserialize(deserializer)?;

    string_map
        .into_iter()
        .map(|(k, v)| {
            Pubkey::from_str(&k)
                .map(|pubkey| (pubkey, v))
                .map_err(|e| serde::de::Error::custom(format!("Invalid pubkey: {e}")))
        })
        .collect()
}

// ----------------------------------------------
// Tests
// ----------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;
    use serde::{Deserialize, Serialize};
    use std::collections::HashMap;

    // Test struct for single pubkey
    #[derive(Debug, Serialize, Deserialize, PartialEq)]
    struct TestPubkey {
        #[serde(
            serialize_with = "serialize_pubkey_as_string",
            deserialize_with = "deserialize_pubkey_from_string"
        )]
        pubkey: Pubkey,
    }

    // Test struct for pubkey list
    #[derive(Debug, Serialize, Deserialize, PartialEq)]
    struct TestPubkeyList {
        #[serde(
            serialize_with = "serialize_pubkeylist_as_string",
            deserialize_with = "deserialize_pubkeylist_from_string"
        )]
        pubkeys: Vec<Pubkey>,
    }

    // Test struct for HashMap with Pubkey keys
    #[derive(Debug, Serialize, Deserialize, PartialEq)]
    struct TestPubkeyMap {
        #[serde(
            serialize_with = "serialize_pubkey_map",
            deserialize_with = "deserialize_pubkey_map"
        )]
        data: HashMap<Pubkey, String>,
    }

    #[test]
    fn test_pubkey_serialization_roundtrip() {
        let original = TestPubkey {
            pubkey: Pubkey::new_unique(),
        };

        let json = serde_json::to_string(&original).unwrap();
        let deserialized: TestPubkey = serde_json::from_str(&json).unwrap();

        assert_eq!(original, deserialized);
    }

    #[test]
    fn test_pubkey_json_format() {
        let pubkey = Pubkey::new_unique();
        let test = TestPubkey { pubkey };

        let json = serde_json::to_string(&test).unwrap();
        let expected = format!("{{\"pubkey\":\"{pubkey}\"}}");

        assert_eq!(json, expected);
    }

    #[test]
    fn test_pubkeylist_serialization_roundtrip() {
        let original = TestPubkeyList {
            pubkeys: vec![
                Pubkey::new_unique(),
                Pubkey::new_unique(),
                Pubkey::new_unique(),
            ],
        };

        let json = serde_json::to_string(&original).unwrap();
        let deserialized: TestPubkeyList = serde_json::from_str(&json).unwrap();

        assert_eq!(original, deserialized);
    }

    #[test]
    fn test_empty_pubkeylist() {
        let original = TestPubkeyList { pubkeys: vec![] };

        let json = serde_json::to_string(&original).unwrap();
        assert_eq!(json, "{\"pubkeys\":\"\"}");

        let deserialized: TestPubkeyList = serde_json::from_str(&json).unwrap();
        assert_eq!(original, deserialized);
    }

    #[test]
    fn test_single_pubkey_in_list() {
        let pk1 = Pubkey::new_unique();
        let pk2 = Pubkey::new_unique();
        let original = TestPubkeyList {
            pubkeys: vec![pk1, pk2],
        };

        let json = serde_json::to_string(&original).unwrap();
        let expected = format!("{{\"pubkeys\":\"{pk1}, {pk2}\"}}");
        assert_eq!(json, expected);

        let deserialized: TestPubkeyList = serde_json::from_str(&json).unwrap();
        assert_eq!(original, deserialized);
    }

    #[test]
    fn test_invalid_pubkey_deserialization() {
        let json = "{\"pubkey\":\"invalid_pubkey\"}";
        let result: Result<TestPubkey, _> = serde_json::from_str(json);

        assert!(result.is_err());
        let err = result.unwrap_err().to_string();
        assert!(err.contains("Invalid"));
    }

    #[test]
    fn test_invalid_pubkey_in_list() {
        let json = "{\"pubkeys\":\"11111111111111111111111111111111, invalid_key\"}";
        let result: Result<TestPubkeyList, _> = serde_json::from_str(json);

        assert!(result.is_err());
    }

    #[test]
    fn test_pubkey_map_serialization_roundtrip() {
        let mut data = HashMap::new();
        let key1 = Pubkey::new_unique();
        let key2 = Pubkey::new_unique();
        data.insert(key1, "value1".to_string());
        data.insert(key2, "value2".to_string());

        let original = TestPubkeyMap { data };

        let json = serde_json::to_string(&original).unwrap();
        let deserialized: TestPubkeyMap = serde_json::from_str(&json).unwrap();

        assert_eq!(original, deserialized);
    }

    #[test]
    fn test_empty_pubkey_map() {
        let original = TestPubkeyMap {
            data: HashMap::new(),
        };

        let json = serde_json::to_string(&original).unwrap();
        assert_eq!(json, "{\"data\":{}}");

        let deserialized: TestPubkeyMap = serde_json::from_str(&json).unwrap();
        assert_eq!(original, deserialized);
    }

    #[test]
    fn test_pubkey_map_json_structure() {
        let mut data = HashMap::new();
        let key = Pubkey::new_unique();
        data.insert(key, "test_value".to_string());

        let test = TestPubkeyMap { data };
        let json = serde_json::to_string(&test).unwrap();

        // Verify the JSON has string keys
        let parsed: serde_json::Value = serde_json::from_str(&json).unwrap();
        assert!(parsed["data"].is_object());
        assert!(parsed["data"][key.to_string()].is_string());
    }

    #[test]
    fn test_bandwidth_roundtrip() {
        #[derive(Debug, Serialize, Deserialize, PartialEq)]
        struct TestBandwidth {
            #[serde(
                serialize_with = "serialize_bandwidth_as_string",
                deserialize_with = "deserialize_bandwidth_from_string"
            )]
            bandwidth: u64,
        }

        let test_cases = vec![
            500,           // 500bps
            5_000,         // 5Kbps
            100_000_000,   // 100Mbps
            2_000_000_000, // 2Gbps
        ];

        for bandwidth in test_cases {
            let original = TestBandwidth { bandwidth };
            let json = serde_json::to_string(&original).unwrap();
            let deserialized: TestBandwidth = serde_json::from_str(&json).unwrap();
            assert_eq!(original, deserialized);
        }
    }

    // Test struct for BTreeMap with Pubkey keys
    #[derive(Debug, Serialize, Deserialize, PartialEq)]
    struct TestPubkeyBTreeMap {
        #[serde(
            serialize_with = "serialize_pubkey_btreemap",
            deserialize_with = "deserialize_pubkey_btreemap"
        )]
        data: BTreeMap<Pubkey, String>,
    }

    #[test]
    fn test_pubkey_btreemap_serialization_roundtrip() {
        let mut data = BTreeMap::new();
        let key1 = Pubkey::new_unique();
        let key2 = Pubkey::new_unique();
        let key3 = Pubkey::new_unique();
        data.insert(key1, "value1".to_string());
        data.insert(key2, "value2".to_string());
        data.insert(key3, "value3".to_string());

        let original = TestPubkeyBTreeMap { data };

        let json = serde_json::to_string(&original).unwrap();
        let deserialized: TestPubkeyBTreeMap = serde_json::from_str(&json).unwrap();

        assert_eq!(original, deserialized);
    }

    #[test]
    fn test_empty_pubkey_btreemap() {
        let original = TestPubkeyBTreeMap {
            data: BTreeMap::new(),
        };

        let json = serde_json::to_string(&original).unwrap();
        assert_eq!(json, "{\"data\":{}}");

        let deserialized: TestPubkeyBTreeMap = serde_json::from_str(&json).unwrap();
        assert_eq!(original, deserialized);
    }

    #[test]
    fn test_pubkey_btreemap_json_structure() {
        let mut data = BTreeMap::new();
        let key = Pubkey::new_unique();
        data.insert(key, "test_value".to_string());

        let test = TestPubkeyBTreeMap { data };
        let json = serde_json::to_string(&test).unwrap();

        // Verify the JSON has string keys
        let parsed: serde_json::Value = serde_json::from_str(&json).unwrap();
        assert!(parsed["data"].is_object());
        assert!(parsed["data"][key.to_string()].is_string());
        assert_eq!(parsed["data"][key.to_string()], "test_value");
    }

    #[test]
    fn test_pubkey_btreemap_ordering() {
        let mut data = BTreeMap::new();

        // Create pubkeys and insert them in random order
        let key1 = Pubkey::new_unique();
        let key2 = Pubkey::new_unique();
        let key3 = Pubkey::new_unique();

        data.insert(key2, "second".to_string());
        data.insert(key1, "first".to_string());
        data.insert(key3, "third".to_string());

        let original = TestPubkeyBTreeMap { data };

        // Serialize and deserialize
        let json = serde_json::to_string(&original).unwrap();
        let deserialized: TestPubkeyBTreeMap = serde_json::from_str(&json).unwrap();

        // BTreeMap should maintain its ordering
        assert_eq!(original.data.len(), deserialized.data.len());
        assert_eq!(original, deserialized);

        // Verify keys are preserved
        for (key, value) in &original.data {
            assert_eq!(deserialized.data.get(key), Some(value));
        }
    }

    #[test]
    fn test_invalid_pubkey_in_btreemap() {
        let json = "{\"data\":{\"invalid_pubkey\":\"value\"}}";
        let result: Result<TestPubkeyBTreeMap, _> = serde_json::from_str(json);

        assert!(result.is_err());
        let err = result.unwrap_err().to_string();
        assert!(err.contains("Invalid pubkey"));
    }

    #[test]
    fn test_pubkey_btreemap_with_complex_values() {
        #[derive(Debug, Serialize, Deserialize, PartialEq, Clone)]
        struct ComplexValue {
            id: u32,
            name: String,
            active: bool,
        }

        #[derive(Debug, Serialize, Deserialize, PartialEq)]
        struct TestComplexBTreeMap {
            #[serde(
                serialize_with = "serialize_pubkey_btreemap",
                deserialize_with = "deserialize_pubkey_btreemap"
            )]
            data: BTreeMap<Pubkey, ComplexValue>,
        }

        let mut data = BTreeMap::new();
        let key1 = Pubkey::new_unique();
        let key2 = Pubkey::new_unique();

        data.insert(
            key1,
            ComplexValue {
                id: 1,
                name: "Alice".to_string(),
                active: true,
            },
        );
        data.insert(
            key2,
            ComplexValue {
                id: 2,
                name: "Bob".to_string(),
                active: false,
            },
        );

        let original = TestComplexBTreeMap { data };

        let json = serde_json::to_string(&original).unwrap();
        let deserialized: TestComplexBTreeMap = serde_json::from_str(&json).unwrap();

        assert_eq!(original, deserialized);
    }
}
