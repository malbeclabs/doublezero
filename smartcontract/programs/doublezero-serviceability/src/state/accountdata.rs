use crate::{
    error::DoubleZeroError,
    state::{
        accounttype::AccountType, contributor::Contributor, device::Device, exchange::Exchange,
        globalconfig::GlobalConfig, globalstate::GlobalState, link::Link, location::Location,
        multicastgroup::MulticastGroup, programconfig::ProgramConfig, user::User,
    },
};

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
    ProgramConfig(ProgramConfig),
    Contributor(Contributor),
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
            AccountData::ProgramConfig(_) => "ProgramConfig",
            AccountData::Contributor(_) => "Contributor",
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
            AccountData::ProgramConfig(program_config) => program_config.to_string(),
            AccountData::Contributor(contributor) => contributor.to_string(),
        }
    }

    pub fn get_global_state(&self) -> Result<GlobalState, DoubleZeroError> {
        if let AccountData::GlobalState(global_state) = self {
            Ok(global_state.clone())
        } else {
            Err(DoubleZeroError::InvalidAccountType)
        }
    }

    pub fn get_global_config(&self) -> Result<GlobalConfig, DoubleZeroError> {
        if let AccountData::GlobalConfig(global_config) = self {
            Ok(global_config.clone())
        } else {
            Err(DoubleZeroError::InvalidAccountType)
        }
    }

    pub fn get_location(&self) -> Result<Location, DoubleZeroError> {
        if let AccountData::Location(location) = self {
            Ok(location.clone())
        } else {
            Err(DoubleZeroError::InvalidAccountType)
        }
    }

    pub fn get_exchange(&self) -> Result<Exchange, DoubleZeroError> {
        if let AccountData::Exchange(exchange) = self {
            Ok(exchange.clone())
        } else {
            Err(DoubleZeroError::InvalidAccountType)
        }
    }

    pub fn get_device(&self) -> Result<Device, DoubleZeroError> {
        if let AccountData::Device(device) = self {
            Ok(device.clone())
        } else {
            Err(DoubleZeroError::InvalidAccountType)
        }
    }

    pub fn get_tunnel(&self) -> Result<Link, DoubleZeroError> {
        if let AccountData::Link(tunnel) = self {
            Ok(tunnel.clone())
        } else {
            Err(DoubleZeroError::InvalidAccountType)
        }
    }

    pub fn get_user(&self) -> Result<User, DoubleZeroError> {
        if let AccountData::User(user) = self {
            Ok(user.clone())
        } else {
            Err(DoubleZeroError::InvalidAccountType)
        }
    }

    pub fn get_multicastgroup(&self) -> Result<MulticastGroup, DoubleZeroError> {
        if let AccountData::MulticastGroup(multicastgroup) = self {
            Ok(multicastgroup.clone())
        } else {
            Err(DoubleZeroError::InvalidAccountType)
        }
    }

    pub fn get_program_config(&self) -> Result<ProgramConfig, DoubleZeroError> {
        if let AccountData::ProgramConfig(program_config) = self {
            Ok(program_config.clone())
        } else {
            Err(DoubleZeroError::InvalidAccountType)
        }
    }

    pub fn get_contributor(&self) -> Result<Contributor, DoubleZeroError> {
        if let AccountData::Contributor(contributor) = self {
            Ok(contributor.clone())
        } else {
            Err(DoubleZeroError::InvalidAccountType)
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
            AccountType::ProgramConfig => AccountData::ProgramConfig(ProgramConfig::from(bytes)),
            AccountType::Contributor => AccountData::Contributor(Contributor::from(bytes)),
        }
    }
}
