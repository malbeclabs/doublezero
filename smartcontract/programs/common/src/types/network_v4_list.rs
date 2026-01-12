use crate::types::NetworkV4;
use borsh::{BorshDeserialize, BorshSerialize};
use byteorder::{LittleEndian, ReadBytesExt};
use serde::{Deserialize, Serialize};
use std::{
    fmt::{Display, Formatter},
    ops::{Index, IndexMut},
    str::FromStr,
};

#[derive(Clone, Debug, Default, Eq, PartialEq, Serialize, Deserialize)]
pub struct NetworkV4List(Vec<NetworkV4>);

impl NetworkV4List {
    pub fn is_empty(&self) -> bool {
        self.0.is_empty()
    }

    pub fn len(&self) -> usize {
        self.0.len()
    }

    pub fn iter(&self) -> impl Iterator<Item = &NetworkV4> {
        self.0.iter()
    }

    pub fn push(&mut self, value: NetworkV4) -> &mut Self {
        self.0.push(value);
        self
    }
}

impl Index<usize> for NetworkV4List {
    type Output = NetworkV4;

    fn index(&self, index: usize) -> &Self::Output {
        &self.0[index]
    }
}

impl IndexMut<usize> for NetworkV4List {
    fn index_mut(&mut self, index: usize) -> &mut Self::Output {
        &mut self.0[index]
    }
}

impl BorshDeserialize for NetworkV4List {
    fn deserialize_reader<R: std::io::Read>(reader: &mut R) -> borsh::io::Result<Self> {
        let len = reader.read_u32::<LittleEndian>()?;
        let mut nets = Vec::with_capacity(len as usize);
        for _ in 0..len {
            let net = NetworkV4::deserialize_reader(reader)?;
            nets.push(net);
        }
        Ok(NetworkV4List(nets))
    }
}

impl BorshSerialize for NetworkV4List {
    fn serialize<W: std::io::Write>(&self, writer: &mut W) -> borsh::io::Result<()> {
        let count = self.0.len() as u32;
        writer.write_all(&count.to_le_bytes())?;
        for net in &self.0 {
            borsh::BorshSerialize::serialize(net, writer)?;
        }
        Ok(())
    }
}

impl Display for NetworkV4List {
    fn fmt(&self, f: &mut Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "{}",
            self.0
                .iter()
                .map(|net| net.to_string())
                .collect::<Vec<String>>()
                .join(", ")
        )
    }
}

impl From<NetworkV4List> for Vec<NetworkV4> {
    fn from(nets: NetworkV4List) -> Self {
        nets.0
    }
}

impl From<Vec<NetworkV4>> for NetworkV4List {
    fn from(nets: Vec<NetworkV4>) -> Self {
        NetworkV4List(nets)
    }
}

impl FromStr for NetworkV4List {
    type Err = String;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        let network_results = s
            .split(',')
            .map(|net| net.trim().parse())
            .collect::<Vec<_>>();

        if network_results.is_empty() {
            return Err(format!("Invalid network '{s}'"));
        }

        let mut networks: Vec<NetworkV4> = vec![];
        for result in network_results {
            match result {
                Ok(net) => networks.push(net),
                Err(e) => return Err(format!("Invalid network '{s}': {e}")),
            }
        }

        Ok(NetworkV4List(networks))
    }
}
