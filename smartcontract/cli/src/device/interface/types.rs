use clap::ValueEnum;
use doublezero_serviceability::state;

#[derive(Clone, Debug, PartialEq, ValueEnum, Default)]
pub enum InterfaceDIA {
    #[default]
    None,
    DIA,
}

impl From<InterfaceDIA> for state::interface::InterfaceDIA {
    fn from(value: InterfaceDIA) -> Self {
        match value {
            InterfaceDIA::None => state::interface::InterfaceDIA::None,
            InterfaceDIA::DIA => state::interface::InterfaceDIA::DIA,
        }
    }
}

#[derive(Clone, Debug, PartialEq, ValueEnum)]
pub enum LoopbackType {
    Vpnv4,
    Ipv4,
    PimRpAddr,
}

impl From<LoopbackType> for state::interface::LoopbackType {
    fn from(value: LoopbackType) -> Self {
        match value {
            LoopbackType::Vpnv4 => state::interface::LoopbackType::Vpnv4,
            LoopbackType::Ipv4 => state::interface::LoopbackType::Ipv4,
            LoopbackType::PimRpAddr => state::interface::LoopbackType::PimRpAddr,
        }
    }
}

#[derive(Clone, Debug, PartialEq, ValueEnum)]
pub enum InterfaceCYOA {
    GREOverDIA = 1,
    GREOverFabric = 2,
    GREOverPrivatePeering = 3,
    GREOverPublicPeering = 4,
    GREOverCable = 5,
}

impl From<InterfaceCYOA> for state::interface::InterfaceCYOA {
    fn from(value: InterfaceCYOA) -> Self {
        match value {
            InterfaceCYOA::GREOverDIA => state::interface::InterfaceCYOA::GREOverDIA,
            InterfaceCYOA::GREOverFabric => state::interface::InterfaceCYOA::GREOverFabric,
            InterfaceCYOA::GREOverPrivatePeering => {
                state::interface::InterfaceCYOA::GREOverPrivatePeering
            }
            InterfaceCYOA::GREOverPublicPeering => {
                state::interface::InterfaceCYOA::GREOverPublicPeering
            }
            InterfaceCYOA::GREOverCable => state::interface::InterfaceCYOA::GREOverCable,
        }
    }
}

#[derive(Clone, Debug, PartialEq, ValueEnum)]
pub enum RoutingMode {
    Static = 0,
    BGP = 1,
}

impl From<RoutingMode> for state::interface::RoutingMode {
    fn from(value: RoutingMode) -> Self {
        match value {
            RoutingMode::Static => state::interface::RoutingMode::Static,
            RoutingMode::BGP => state::interface::RoutingMode::BGP,
        }
    }
}
