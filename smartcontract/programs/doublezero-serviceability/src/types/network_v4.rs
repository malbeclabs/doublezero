use borsh::{BorshDeserialize, BorshSerialize};
use ipnetwork::{IpNetworkError, Ipv4Network};
use std::{
    fmt::{Display, Formatter},
    net::Ipv4Addr,
    str::FromStr,
};

#[cfg(feature = "serde")]
use serde::{Deserialize, Deserializer, Serialize};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct NetworkV4(Ipv4Network);

impl NetworkV4 {
    pub fn new(ip: Ipv4Addr, prefix: u8) -> Result<Self, IpNetworkError> {
        Ok(NetworkV4(Ipv4Network::new(ip, prefix)?))
    }

    pub fn ip(&self) -> Ipv4Addr {
        self.0.ip()
    }

    pub fn prefix(&self) -> u8 {
        self.0.prefix()
    }
}

impl Default for NetworkV4 {
    fn default() -> Self {
        NetworkV4(Ipv4Network::new(Ipv4Addr::UNSPECIFIED, 0).unwrap())
    }
}

impl Display for NetworkV4 {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        self.0.fmt(f)
    }
}

impl From<NetworkV4> for Ipv4Network {
    fn from(net: NetworkV4) -> Self {
        net.0
    }
}

impl From<Ipv4Network> for NetworkV4 {
    fn from(net: Ipv4Network) -> Self {
        NetworkV4(net)
    }
}

impl FromStr for NetworkV4 {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        let net = Ipv4Network::from_str(s)
            .map_err(|e| format!("Invalid network address format '{s}': {e}"))?;
        Ok(NetworkV4(net))
    }
}

impl BorshDeserialize for NetworkV4 {
    fn deserialize_reader<R: std::io::Read>(reader: &mut R) -> borsh::io::Result<Self> {
        let mut data = [0u8; 5];
        reader.read_exact(&mut data)?;
        let ip = Ipv4Addr::from(<[u8; 4]>::try_from(&data[0..4]).map_err(|e| {
            borsh::io::Error::new(
                borsh::io::ErrorKind::InvalidData,
                format!("Invalid IP data: {e}"),
            )
        })?);
        NetworkV4::new(ip, data[4]).map_err(|e| {
            borsh::io::Error::new(
                borsh::io::ErrorKind::InvalidData,
                format!("Invalid network address: {e}"),
            )
        })
    }
}

impl BorshSerialize for NetworkV4 {
    fn serialize<W: std::io::Write>(&self, writer: &mut W) -> borsh::io::Result<()> {
        let ip = self.0.ip().octets();
        writer.write_all(&ip)?;
        writer.write_all(&[self.0.prefix()])?;
        Ok(())
    }
}

#[cfg(feature = "serde")]
impl<'de> Deserialize<'de> for NetworkV4 {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        let s: String = <String as serde::Deserialize<'de>>::deserialize(deserializer)?;
        NetworkV4::from_str(&s).map_err(serde::de::Error::custom)
    }
}

#[cfg(feature = "serde")]
impl Serialize for NetworkV4 {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: serde::Serializer,
    {
        serde::Serialize::serialize(&self.0.to_string(), serializer)
    }
}
