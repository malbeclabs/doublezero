use crate::{
    error::DoubleZeroError,
    state::{
        accesspass::AccessPass, accounttype::AccountType, contributor::Contributor, device::Device,
        exchange::Exchange, globalconfig::GlobalConfig, globalstate::GlobalState, link::Link,
        location::Location, multicastgroup::MulticastGroup, programconfig::ProgramConfig,
        user::User,
    },
};
use solana_program::program_error::ProgramError;

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
    AccessPass(AccessPass),
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
            AccountData::AccessPass(_) => "AccessPass",
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
            AccountData::AccessPass(access_pass) => access_pass.to_string(),
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

    pub fn get_accesspass(&self) -> Result<AccessPass, DoubleZeroError> {
        if let AccountData::AccessPass(accesspass) = self {
            Ok(accesspass.clone())
        } else {
            Err(DoubleZeroError::InvalidAccountType)
        }
    }
}

impl TryFrom<&[u8]> for AccountData {
    type Error = ProgramError;

    fn try_from(bytes: &[u8]) -> Result<Self, Self::Error> {
        match AccountType::from(bytes[0]) {
            AccountType::None => Ok(AccountData::None),
            AccountType::GlobalState => Ok(AccountData::GlobalState(GlobalState::try_from(bytes)?)),
            AccountType::Config => Ok(AccountData::GlobalConfig(GlobalConfig::try_from(bytes)?)),
            AccountType::Location => Ok(AccountData::Location(Location::try_from(bytes)?)),
            AccountType::Exchange => Ok(AccountData::Exchange(Exchange::try_from(bytes)?)),
            AccountType::Device => Ok(AccountData::Device(Device::try_from(bytes)?)),
            AccountType::Link => Ok(AccountData::Link(Link::try_from(bytes)?)),
            AccountType::User => Ok(AccountData::User(User::try_from(bytes)?)),
            AccountType::MulticastGroup => Ok(AccountData::MulticastGroup(
                MulticastGroup::try_from(bytes)?,
            )),
            AccountType::ProgramConfig => {
                Ok(AccountData::ProgramConfig(ProgramConfig::try_from(bytes)?))
            }
            AccountType::Contributor => Ok(AccountData::Contributor(Contributor::try_from(bytes)?)),
            AccountType::AccessPass => Ok(AccountData::AccessPass(AccessPass::try_from(bytes)?)),
        }
    }
}
