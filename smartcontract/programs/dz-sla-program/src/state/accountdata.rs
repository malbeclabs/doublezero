use super::{
    accounttype::AccountType, device::Device, exchange::Exchange, globalconfig::GlobalConfig,
    globalstate::GlobalState, location::Location, tunnel::Tunnel, user::User,
};

#[derive(Debug, PartialEq)]
pub enum AccountData {
    None,
    GlobalState(GlobalState),
    GlobalConfig(GlobalConfig),
    Location(Location),
    Exchange(Exchange),
    Device(Device),
    Tunnel(Tunnel),
    User(User),
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
            AccountData::Tunnel(_) => "Tunnel",
            AccountData::User(_) => "User",
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
            AccountData::Tunnel(tunnel) => tunnel.to_string(),
            AccountData::User(user) => user.to_string(),
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

    pub fn get_tunnel(&self) -> Tunnel {
        if let AccountData::Tunnel(tunnel) = self {
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
            AccountType::Tunnel => AccountData::Tunnel(Tunnel::from(bytes)),
            AccountType::User => AccountData::User(User::from(bytes)),
        }
    }
}
