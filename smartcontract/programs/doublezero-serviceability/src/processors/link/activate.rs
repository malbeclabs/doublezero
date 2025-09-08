use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    helper::*,
    state::{device::*, link::*},
};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
use doublezero_program_common::types::NetworkV4;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone, Default)]
pub struct LinkActivateArgs {
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
}

impl fmt::Debug for LinkActivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "tunnel_id: {}, tunnel_net: {}",
            self.tunnel_id, &self.tunnel_net,
        )
    }
}

pub fn process_activate_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkActivateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let link_account = next_account_info(accounts_iter)?;
    let side_a_device_account = next_account_info(accounts_iter)?;
    let side_z_device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_activate_link({:?})", value);

    // Check the owner of the accounts
    assert_eq!(link_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        side_a_device_account.owner, program_id,
        "Invalid PDA Account Owner for Side A Device"
    );
    assert_eq!(
        side_z_device_account.owner, program_id,
        "Invalid PDA Account Owner for Side Z Device"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    // Check if the account is writable
    assert!(link_account.is_writable, "PDA Account is not writable");
    assert!(
        side_a_device_account.is_writable,
        "Side A PDA Account is not writable"
    );
    assert!(
        side_z_device_account.is_writable,
        "Side Z PDA Account is not writable"
    );

    let globalstate = globalstate_get(globalstate_account)?;
    if globalstate.activator_authority_pk != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut link: Link = Link::try_from(link_account)?;
    let mut side_a_dev: Device = Device::try_from(side_a_device_account)?;
    let mut side_z_dev: Device = Device::try_from(side_z_device_account)?;

    if link.status != LinkStatus::Pending {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    let (idx_a, side_a_iface) = side_a_dev
        .find_interface(&link.side_a_iface_name)
        .map_err(|_| DoubleZeroError::InterfaceNotFound)?;

    let (idx_z, side_z_iface) = side_z_dev
        .find_interface(&link.side_z_iface_name)
        .map_err(|_| DoubleZeroError::InterfaceNotFound)?;

    if side_a_iface.status != InterfaceStatus::Unlinked
        || side_z_iface.status != InterfaceStatus::Unlinked
    {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    let mut updated_iface_a = side_a_iface.clone();
    updated_iface_a.status = InterfaceStatus::Activated;
    updated_iface_a.ip_net =
        NetworkV4::new(value.tunnel_net.nth(0).unwrap(), value.tunnel_net.prefix()).unwrap();
    side_a_dev.interfaces[idx_a] = Interface::V1(updated_iface_a);

    let mut updated_iface_z = side_z_iface.clone();
    updated_iface_z.status = InterfaceStatus::Activated;
    updated_iface_z.ip_net =
        NetworkV4::new(value.tunnel_net.nth(1).unwrap(), value.tunnel_net.prefix()).unwrap();
    side_z_dev.interfaces[idx_z] = Interface::V1(updated_iface_z);

    link.tunnel_id = value.tunnel_id;
    link.tunnel_net = value.tunnel_net;
    link.status = LinkStatus::Activated;

    account_write(
        side_a_device_account,
        &side_a_dev,
        payer_account,
        system_program,
    )?;
    account_write(
        side_z_device_account,
        &side_z_dev,
        payer_account,
        system_program,
    )?;
    account_write(link_account, &link, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Activated: {:?}", link);

    Ok(())
}
