use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get_next,
    helper::account_write,
    state::{accounttype::AccountType, contributor::Contributor, device::*},
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use doublezero_program_common::{types::NetworkV4, validate_iface};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone, Default)]
pub struct DeviceInterfaceCreateArgs {
    pub name: String,
    pub loopback_type: LoopbackType,
    pub vlan_id: u16,
    pub user_tunnel_endpoint: bool,
}

impl fmt::Debug for DeviceInterfaceCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "name: {}, loopback_type: {}, vlan_id: {}, user_tunnel_endpoint: {}",
            self.name, self.loopback_type, self.vlan_id, self.user_tunnel_endpoint
        )
    }
}

pub fn process_create_device_interface(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceInterfaceCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_device_interface({:?})", value);

    let name = validate_iface(&value.name).map_err(|_| DoubleZeroError::InvalidInterfaceName)?;

    assert_eq!(
        contributor_account.owner, program_id,
        "Invalid Contributor Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );

    assert!(device_account.is_writable, "PDA Account is not writable");

    let globalstate = globalstate_get_next(globalstate_account)?;
    assert_eq!(globalstate.account_type, AccountType::GlobalState);

    let contributor = Contributor::try_from(contributor_account)?;

    if contributor.owner != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::InvalidOwnerPubkey.into());
    }

    let mut interface_type = InterfaceType::Physical;
    if value.name.starts_with("Loopback") {
        interface_type = InterfaceType::Loopback;
    }

    let mut device: Device = Device::try_from(device_account)?;
    device
        .interfaces
        .push(Interface::V1(CurrentInterfaceVersion {
            status: InterfaceStatus::Pending,
            name,
            interface_type,
            loopback_type: value.loopback_type,
            vlan_id: value.vlan_id,
            ip_net: NetworkV4::default(),
            node_segment_idx: 0,
            user_tunnel_endpoint: value.user_tunnel_endpoint,
        }));

    account_write(device_account, &device, payer_account, system_program)?;

    Ok(())
}
