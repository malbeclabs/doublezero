use borsh::{BorshDeserialize, BorshSerialize};
use ipnetwork::{IpNetworkError, Ipv4Network};
use serde::{Deserialize, Deserializer, Serialize};
use std::{
    fmt::{Display, Formatter},
    hash::{Hash, Hasher},
    net::Ipv4Addr,
    str::FromStr,
};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct NetworkV4(Ipv4Network);

impl Hash for NetworkV4 {
    fn hash<H: Hasher>(&self, state: &mut H) {
        self.0.hash(state);
    }
}

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

    pub fn nth(&self, n: u32) -> Option<Ipv4Addr> {
        self.0.nth(n)
    }

    pub fn size(&self) -> u32 {
        self.0.size()
    }

    pub fn contains(&self, ip: Ipv4Addr) -> bool {
        self.0.contains(ip)
    }

    pub fn is_subnet_of(&self, other: &NetworkV4) -> bool {
        self.0.is_subnet_of(other.0)
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

impl<'de> Deserialize<'de> for NetworkV4 {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: Deserializer<'de>,
    {
        let s: String = <String as serde::Deserialize<'de>>::deserialize(deserializer)?;
        NetworkV4::from_str(&s).map_err(serde::de::Error::custom)
    }
}

impl Serialize for NetworkV4 {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: serde::Serializer,
    {
        serde::Serialize::serialize(&self.0.to_string(), serializer)
    }
}
