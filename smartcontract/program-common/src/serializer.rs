use crate::types::{parse_utils::bandwidth_to_string, NetworkV4List};
use solana_program::pubkey::Pubkey;

pub fn serialize_pubkey_as_string<S>(pubkey: &Pubkey, serializer: S) -> Result<S::Ok, S::Error>
where
    S: serde::Serializer,
{
    serializer.serialize_str(&pubkey.to_string())
}

pub fn serialize_pubkeylist_as_string<S>(
    pubkey: &[Pubkey],
    serializer: S,
) -> Result<S::Ok, S::Error>
where
    S: serde::Serializer,
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
    S: serde::Serializer,
{
    serializer.serialize_str(list.to_string().as_str())
}
