use clap::ValueEnum;
use doublezero_serviceability::state::device;

#[derive(Clone, Debug, PartialEq, ValueEnum)]
pub enum InterfaceType {
    Loopback,
    Physical,
}

impl From<InterfaceType> for device::InterfaceType {
    fn from(value: InterfaceType) -> Self {
        match value {
            InterfaceType::Loopback => device::InterfaceType::Loopback,
            InterfaceType::Physical => device::InterfaceType::Physical,
        }
    }
}

#[derive(Clone, Debug, PartialEq, ValueEnum)]
pub enum LoopbackType {
    None,
    Vpnv4,
    Ipv4,
    PimRpAddr,
    Reserved,
}

impl From<LoopbackType> for device::LoopbackType {
    fn from(value: LoopbackType) -> Self {
        match value {
            LoopbackType::None => device::LoopbackType::None,
            LoopbackType::Vpnv4 => device::LoopbackType::Vpnv4,
            LoopbackType::Ipv4 => device::LoopbackType::Ipv4,
            LoopbackType::PimRpAddr => device::LoopbackType::PimRpAddr,
            LoopbackType::Reserved => device::LoopbackType::Reserved,
        }
    }
}
