use super::{
    accounttype::AccountType, device::Device, exchange::Exchange, globalconfig::GlobalConfig,
    globalstate::GlobalState, location::Location, multicastgroup::MulticastGroup, user::User,
};
use crate::state::link::Link;

#[derive(Debug, PartialEq)]
pub enum AccountData {
    None,
    GlobalState(GlobalState),
    GlobalConfig(GlobalConfig),
    Location(Location),
    Exchange(Exchange),
    Device(Device),
    Link(Link),
    User(User),
    MulticastGroup(MulticastGroup),
}

impl AccountData {
    pub fn get_name(&self) -> &str {
        match self {
            AccountData::None => "None",
            AccountData::GlobalState(_) => "GlobalState",
            AccountData::GlobalConfig(_) => "GlobalConfig",
            AccountData::Location(_) => "Location",
            AccountData::Exchange(_) => "Exchange",
            AccountData::Device(_) => "Device",
            AccountData::Link(_) => "Link",
            AccountData::User(_) => "User",
            AccountData::MulticastGroup(_) => "MulticastGroup",
        }
    }

    pub fn get_args(&self) -> String {
        match self {
            AccountData::None => "".to_string(),
            AccountData::GlobalState(global_state) => global_state.to_string(),
            AccountData::GlobalConfig(global_config) => global_config.to_string(),
            AccountData::Location(location) => location.to_string(),
            AccountData::Exchange(exchange) => exchange.to_string(),
            AccountData::Device(device) => device.to_string(),
            AccountData::Link(tunnel) => tunnel.to_string(),
            AccountData::User(user) => user.to_string(),
            AccountData::MulticastGroup(multicast_group) => multicast_group.to_string(),
        }
    }

    pub fn get_global_state(&self) -> GlobalState {
        if let AccountData::GlobalState(global_state) = self {
            global_state.clone()
        } else {
            panic!("Invalid Account Type")
        }
    }

    pub fn get_global_config(&self) -> GlobalConfig {
        if let AccountData::GlobalConfig(global_config) = self {
            global_config.clone()
        } else {
            panic!("Invalid Account Type")
        }
    }

    pub fn get_location(&self) -> Location {
        if let AccountData::Location(location) = self {
            location.clone()
        } else {
            panic!("Invalid Account Type")
        }
    }

    pub fn get_exchange(&self) -> Exchange {
        if let AccountData::Exchange(exchange) = self {
            exchange.clone()
        } else {
            panic!("Invalid Account Type")
        }
    }

    pub fn get_device(&self) -> Device {
        if let AccountData::Device(device) = self {
            device.clone()
        } else {
            panic!("Invalid Account Type")
        }
    }

    pub fn get_tunnel(&self) -> Link {
        if let AccountData::Link(tunnel) = self {
            tunnel.clone()
        } else {
            panic!("Invalid Account Type")
        }
    }

    pub fn get_user(&self) -> User {
        if let AccountData::User(user) = self {
            user.clone()
        } else {
            panic!("Invalid Account Type")
        }
    }

    pub fn get_multicastgroup(&self) -> MulticastGroup {
        if let AccountData::MulticastGroup(multicastgroup) = self {
            multicastgroup.clone()
        } else {
            panic!("Invalid Account Type")
        }
    }
}

impl From<&[u8]> for AccountData {
    fn from(bytes: &[u8]) -> Self {
        match AccountType::from(bytes[0]) {
            AccountType::None => AccountData::None,
            AccountType::GlobalState => AccountData::GlobalState(GlobalState::from(bytes)),
            AccountType::Config => AccountData::GlobalConfig(GlobalConfig::from(bytes)),
            AccountType::Location => AccountData::Location(Location::from(bytes)),
            AccountType::Exchange => AccountData::Exchange(Exchange::from(bytes)),
            AccountType::Device => AccountData::Device(Device::from(bytes)),
            AccountType::Link => AccountData::Link(Link::from(bytes)),
            AccountType::User => AccountData::User(User::from(bytes)),
            AccountType::MulticastGroup => AccountData::MulticastGroup(MulticastGroup::from(bytes)),
        }
    }
}
