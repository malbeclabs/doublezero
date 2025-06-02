pub use doublezero_sla_program::pda::{
    get_device_pda, get_exchange_pda, get_globalconfig_pda, get_link_pda, get_location_pda,
    get_multicastgroup_pda, get_user_pda,
};

pub use doublezero_sla_program::addresses::*;

pub use crate::config::{
    create_new_pubkey_user, get_doublezero_pubkey, read_doublezero_config, write_doublezero_config,
    ClientConfig,
};

pub use doublezero_sla_program::state::{
    accountdata::AccountData,
    accounttype::AccountType,
    device::{Device, DeviceStatus, DeviceType},
    exchange::{Exchange, ExchangeStatus},
    globalconfig::GlobalConfig,
    link::{Link, LinkLinkType, LinkStatus},
    location::{Location, LocationStatus},
    multicastgroup::{MulticastGroup, MulticastGroupStatus},
    user::{User, UserCYOA, UserStatus, UserType},
};
pub use doublezero_sla_program::types::*;

#[macro_use]
extern crate lazy_static;

mod client;
mod config;
mod consts;
mod doublezeroclient;
mod dztransaction;
mod errors;
mod tests;
mod utils;

pub mod commands;

pub use crate::client::DZClient;

pub use crate::config::{
    convert_program_moniker, convert_url_moniker, convert_url_to_ws, convert_ws_moniker,
};
pub use crate::doublezeroclient::DoubleZeroClient;
pub use crate::doublezeroclient::MockDoubleZeroClient;
pub use crate::errors::*;

pub use crate::commands::globalconfig::get::GetGlobalConfigCommand;
pub use crate::commands::globalstate::get::GetGlobalStateCommand;
pub use crate::commands::location::create::CreateLocationCommand;
pub use crate::commands::location::get::GetLocationCommand;
