pub use double_zero_sla_program::pda::{
    get_device_pda, get_exchange_pda, get_location_pda, get_tunnel_pda,
};

pub use double_zero_sla_program::addresses::*;

pub use crate::config::{
    create_new_pubkey_user, get_doublezero_pubkey, read_doublezero_config, write_doublezero_config,
    ClientConfig,
};

pub use double_zero_sla_program::state::{
    accountdata::AccountData,
    accounttype::AccountType,
    device::{Device, DeviceStatus, DeviceType},
    exchange::{Exchange, ExchangeStatus},
    globalconfig::GlobalConfig,
    location::{Location, LocationStatus},
    tunnel::{Tunnel, TunnelStatus, TunnelTunnelType},
    user::{User, UserCYOA, UserStatus, UserType},
};
pub use double_zero_sla_program::types::*;

#[macro_use]
extern crate lazy_static;

mod consts;
mod client;
mod config;
mod doublezeroclient;
mod errors;
mod servicecontroller;
mod services;
mod utils;
mod dztransaction;
pub mod cli;

pub use crate::client::DZClient;
pub use crate::doublezeroclient::DoubleZeroClient;
pub use crate::services::{
    allowlist::AllowlistService, device::DeviceService, exchange::ExchangeService,
    location::LocationService, tunnel::TunnelService, user::UserService,
};

pub use crate::config::{convert_url_moniker, convert_ws_moniker, convert_program_moniker, convert_url_to_ws};
pub use crate::errors::*;
pub use crate::servicecontroller::{
    service_controller_can_open, service_controller_check, ProvisioningRequest, RemoveTunnelArgs,
    ServiceController,
};

pub use crate::cli::config::*;
pub use crate::cli::config::get::*;
pub use crate::cli::config::set::*;