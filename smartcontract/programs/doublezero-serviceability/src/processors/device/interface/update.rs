use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    helper::*,
    state::{accounttype::AccountType, device::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::{types::NetworkV4, validate_iface};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct DeviceInterfaceUpdateArgs {
    pub name: String,
    pub loopback_type: Option<LoopbackType>,
    pub vlan_id: Option<u16>,
    pub user_tunnel_endpoint: Option<bool>,
    pub status: Option<InterfaceStatus>,
    pub ip_net: Option<NetworkV4>,
    pub node_segment_idx: Option<u16>,
}

impl fmt::Debug for DeviceInterfaceUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "name: {}, ", self.name)?;
        if self.loopback_type.is_some() {
            write!(f, "loopback_type: {}, ", self.loopback_type.unwrap())?;
        }
        if self.vlan_id.is_some() {
            write!(f, "vlan_id: {}, ", self.vlan_id.unwrap())?;
        }
        if self.user_tunnel_endpoint.is_some() {
            write!(
                f,
                "user_tunnel_endpoint: {}, ",
                self.user_tunnel_endpoint.unwrap()
            )?;
        }
        Ok(())
    }
}

pub fn process_update_device_interface(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceInterfaceUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_device_interface({:?})", value);

    // Check the owner of the accounts
    assert_eq!(
        device_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        contributor_account.owner, program_id,
        "Invalid Contributor Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(device_account.is_writable, "PDA Account is not writable");

    let globalstate = globalstate_get(globalstate_account)?;
    assert_eq!(globalstate.account_type, AccountType::GlobalState);

    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut device: Device = Device::try_from(device_account)?;

    let (idx, _) = device
        .find_interface(&value.name)
        .map_err(|_| DoubleZeroError::InterfaceNotFound)?;
    let mut iface = device.interfaces[idx].into_current_version();
    iface.name = validate_iface(&value.name).map_err(|_| DoubleZeroError::InvalidInterfaceName)?;

    if let Some(loopback_type) = value.loopback_type {
        iface.loopback_type = loopback_type;
    }
    if let Some(vlan_id) = value.vlan_id {
        iface.vlan_id = vlan_id;
    }
    if let Some(user_tunnel_endpoint) = value.user_tunnel_endpoint {
        iface.user_tunnel_endpoint = user_tunnel_endpoint;
    }
    if let Some(status) = value.status {
        iface.status = status;
    }
    if let Some(ip_net) = value.ip_net {
        iface.ip_net = ip_net;
    }
    if let Some(node_segment_idx) = value.node_segment_idx {
        iface.node_segment_idx = node_segment_idx;
    }
    device.interfaces[idx] = Interface::V1(iface);

    account_write(device_account, &device, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Updated: {:?}", device);

    Ok(())
}
