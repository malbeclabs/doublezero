use clap::ValueEnum;
use doublezero_serviceability::{processors, state};

#[derive(Clone, Debug, PartialEq, ValueEnum)]
pub enum InterfaceType {
    Loopback,
    Physical,
}

impl From<InterfaceType> for state::interface::InterfaceType {
    fn from(value: InterfaceType) -> Self {
        match value {
            InterfaceType::Loopback => state::interface::InterfaceType::Loopback,
            InterfaceType::Physical => state::interface::InterfaceType::Physical,
        }
    }
}

#[derive(Clone, Debug, PartialEq, ValueEnum)]
pub enum LoopbackType {
    None,
    Vpnv4,
    Ipv4,
    PimRpAddr,
}

impl From<LoopbackType> for state::interface::LoopbackType {
    fn from(value: LoopbackType) -> Self {
        match value {
            LoopbackType::None => state::interface::LoopbackType::None,
            LoopbackType::Vpnv4 => state::interface::LoopbackType::Vpnv4,
            LoopbackType::Ipv4 => state::interface::LoopbackType::Ipv4,
            LoopbackType::PimRpAddr => state::interface::LoopbackType::PimRpAddr,
        }
    }
}

#[derive(Clone, Debug, PartialEq, ValueEnum)]
pub enum InterfaceSubType {
    None = 0,
    CYOA = 1,
    DIA = 2,
}

impl From<InterfaceSubType> for processors::device::interface::InterfaceSubType {
    fn from(value: InterfaceSubType) -> Self {
        match value {
            InterfaceSubType::None => processors::device::interface::InterfaceSubType::None,
            InterfaceSubType::CYOA => processors::device::interface::InterfaceSubType::CYOA,
            InterfaceSubType::DIA => processors::device::interface::InterfaceSubType::DIA,
        }
    }
}
