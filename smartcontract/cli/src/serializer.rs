use doublezero_sdk::{
    bandwidth_to_string, ipv4_to_string, networkv4_list_to_string, networkv4_to_string, IpV4,
    NetworkV4, NetworkV4List,
};
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

pub fn serialize_ipv4_as_string<S>(ip: &IpV4, serializer: S) -> Result<S::Ok, S::Error>
where
    S: serde::Serializer,
{
    serializer.serialize_str(&ipv4_to_string(ip))
}

pub fn serialize_bandwidth_as_string<S>(bandwidth: &u64, serializer: S) -> Result<S::Ok, S::Error>
where
    S: serde::Serializer,
{
    serializer.serialize_str(&bandwidth_to_string(*bandwidth))
}

pub fn serialize_networkv4_as_string<S>(ip: &NetworkV4, serializer: S) -> Result<S::Ok, S::Error>
where
    S: serde::Serializer,
{
    serializer.serialize_str(&networkv4_to_string(ip))
}

pub fn serialize_networkv4list_as_string<S>(
    list: &NetworkV4List,
    serializer: S,
) -> Result<S::Ok, S::Error>
where
    S: serde::Serializer,
{
    serializer.serialize_str(&networkv4_list_to_string(list))
}
