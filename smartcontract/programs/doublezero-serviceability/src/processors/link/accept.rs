use crate::{
    error::DoubleZeroError,
    processors::{
        link::resource_onchain_helpers::validate_and_allocate_link_resources,
        validation::validate_program_account,
    },
    serializer::try_acc_write,
    state::{
        contributor::Contributor,
        device::Device,
        interface::{InterfaceCYOA, InterfaceDIA, InterfaceStatus},
        link::*,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::types::NetworkV4;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct LinkAcceptArgs {
    pub side_z_iface_name: String,
    /// Onchain allocation is mandatory; the field must be `true`. Retained for ABI stability.
    #[incremental(default = false)]
    pub use_onchain_allocation: bool,
}

impl fmt::Debug for LinkAcceptArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "side_z_iface_name: {}, use_onchain_allocation: {}",
            self.side_z_iface_name, self.use_onchain_allocation,
        )
    }
}

pub fn process_accept_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkAcceptArgs,
) -> ProgramResult {
    if !value.use_onchain_allocation {
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    let accounts_iter = &mut accounts.iter();

    // Account layout: [link, contributor, side_z_dev, globalstate, side_a_dev,
    //                  device_tunnel_block, link_ids, payer, system]
    let link_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let side_z_account = next_account_info(accounts_iter)?;
    let _globalstate_account = next_account_info(accounts_iter)?;

    let side_a_device_account = next_account_info(accounts_iter)?;
    let device_tunnel_block_ext = next_account_info(accounts_iter)?;
    let link_ids_ext = next_account_info(accounts_iter)?;

    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_accept_link({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate accounts
    validate_program_account!(link_account, program_id, writable = true, "Link");
    validate_program_account!(
        contributor_account,
        program_id,
        writable = false,
        "Contributor"
    );
    validate_program_account!(side_z_account, program_id, writable = true, "SideZ");

    // Validate Contributor Owner
    let contributor = Contributor::try_from(contributor_account)?;
    if contributor.owner != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Validate Link Status
    let mut link: Link = Link::try_from(link_account)?;
    if link.status != LinkStatus::Requested {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    if link.side_z_pk != *side_z_account.key {
        return Err(DoubleZeroError::InvalidAccountOwner.into());
    }

    // Validate Side Z Device
    let side_z_dev = Device::try_from(side_z_account)?;
    if side_z_dev.contributor_pk != *contributor_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if !side_z_dev
        .interfaces
        .iter()
        .any(|iface| iface.into_current_version().name == value.side_z_iface_name)
    {
        #[cfg(test)]
        msg!("{:?}", side_z_dev);

        return Err(DoubleZeroError::InvalidInterfaceName.into());
    }
    link.side_z_iface_name = value.side_z_iface_name.clone();

    // Combined accept + activate path with onchain allocation.
    validate_program_account!(
        side_a_device_account,
        program_id,
        writable = true,
        "SideADevice"
    );
    assert!(
        side_z_account.is_writable,
        "Side Z PDA Account is not writable"
    );

    let mut side_a_dev = Device::try_from(side_a_device_account)?;
    let mut side_z_dev = Device::try_from(side_z_account)?;

    if link.side_a_pk != *side_a_device_account.key || link.side_z_pk != *side_z_account.key {
        return Err(ProgramError::InvalidAccountData);
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

    if side_a_iface.interface_cyoa != InterfaceCYOA::None
        || side_a_iface.interface_dia != InterfaceDIA::None
        || side_z_iface.interface_cyoa != InterfaceCYOA::None
        || side_z_iface.interface_dia != InterfaceDIA::None
    {
        return Err(DoubleZeroError::InterfaceHasEdgeAssignment.into());
    }

    // Allocate resources (validates ResourceExtension PDAs internally)
    validate_and_allocate_link_resources(
        program_id,
        &mut link,
        device_tunnel_block_ext,
        link_ids_ext,
    )?;

    let mut updated_iface_a = side_a_iface.clone();
    updated_iface_a.status = InterfaceStatus::Activated;
    if updated_iface_a.ip_net == NetworkV4::default() {
        updated_iface_a.ip_net =
            NetworkV4::new(link.tunnel_net.nth(0).unwrap(), link.tunnel_net.prefix()).unwrap();
    }
    side_a_dev.interfaces[idx_a] = updated_iface_a.to_interface();

    let mut updated_iface_z = side_z_iface.clone();
    updated_iface_z.status = InterfaceStatus::Activated;
    if updated_iface_z.ip_net == NetworkV4::default() {
        updated_iface_z.ip_net =
            NetworkV4::new(link.tunnel_net.nth(1).unwrap(), link.tunnel_net.prefix()).unwrap();
    }
    side_z_dev.interfaces[idx_z] = updated_iface_z.to_interface();

    link.status = LinkStatus::Activated;
    link.check_status_transition();

    try_acc_write(&side_a_dev, side_a_device_account, payer_account, accounts)?;
    try_acc_write(&side_z_dev, side_z_account, payer_account, accounts)?;
    try_acc_write(&link, link_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Accepted and Activated: {:?}", link);

    Ok(())
}
