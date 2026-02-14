use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    resource::ResourceType,
    serializer::try_acc_write,
    state::{
        device::*, globalstate::GlobalState, interface::InterfaceStatus, link::*,
        resource_extension::ResourceExtensionBorrowed,
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
pub struct LinkActivateArgs {
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
    /// When true, on-chain allocation is used (ResourceExtension accounts required).
    /// When false, legacy behavior is used (values from args).
    #[incremental(default = false)]
    pub use_onchain_allocation: bool,
}

impl fmt::Debug for LinkActivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "tunnel_id: {}, tunnel_net: {}, use_onchain_allocation: {}",
            self.tunnel_id, &self.tunnel_net, self.use_onchain_allocation,
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

    // Optional: ResourceExtension accounts for on-chain allocation (before payer)
    // Account layout WITH ResourceExtension (use_onchain_allocation = true):
    //   [link, side_a_dev, side_z_dev, globalstate, device_tunnel_block, link_ids, payer, system]
    // Account layout WITHOUT (legacy, use_onchain_allocation = false):
    //   [link, side_a_dev, side_z_dev, globalstate, payer, system]
    let resource_extension_accounts = if value.use_onchain_allocation {
        let device_tunnel_block_ext = next_account_info(accounts_iter)?; // DeviceTunnelBlock (global)
        let link_ids_ext = next_account_info(accounts_iter)?; // LinkIds (global)
        Some((device_tunnel_block_ext, link_ids_ext))
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_activate_link({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

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

    let globalstate = GlobalState::try_from(globalstate_account)?;

    // Authorization: allow activator_authority_pk OR foundation_allowlist
    let is_activator = globalstate.activator_authority_pk == *payer_account.key;
    let is_foundation = globalstate.foundation_allowlist.contains(payer_account.key);
    if !is_activator && !is_foundation {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut link: Link = Link::try_from(link_account)?;
    let mut side_a_dev: Device = Device::try_from(side_a_device_account)?;
    let mut side_z_dev: Device = Device::try_from(side_z_device_account)?;

    if link.side_a_pk != *side_a_device_account.key || link.side_z_pk != *side_z_device_account.key
    {
        return Err(ProgramError::InvalidAccountData);
    }

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

    // Allocate resources from ResourceExtension or use provided values
    if let Some((device_tunnel_block_ext, link_ids_ext)) = resource_extension_accounts {
        // Validate device_tunnel_block_ext (DeviceTunnelBlock - global)
        assert_eq!(
            device_tunnel_block_ext.owner, program_id,
            "Invalid ResourceExtension Account Owner for DeviceTunnelBlock"
        );
        assert!(
            device_tunnel_block_ext.is_writable,
            "ResourceExtension Account for DeviceTunnelBlock is not writable"
        );
        assert!(
            !device_tunnel_block_ext.data_is_empty(),
            "ResourceExtension Account for DeviceTunnelBlock is empty"
        );

        let (expected_device_tunnel_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::DeviceTunnelBlock);
        assert_eq!(
            device_tunnel_block_ext.key, &expected_device_tunnel_pda,
            "Invalid ResourceExtension PDA for DeviceTunnelBlock"
        );

        // Validate link_ids_ext (LinkIds - global)
        assert_eq!(
            link_ids_ext.owner, program_id,
            "Invalid ResourceExtension Account Owner for LinkIds"
        );
        assert!(
            link_ids_ext.is_writable,
            "ResourceExtension Account for LinkIds is not writable"
        );
        assert!(
            !link_ids_ext.data_is_empty(),
            "ResourceExtension Account for LinkIds is empty"
        );

        let (expected_link_ids_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::LinkIds);
        assert_eq!(
            link_ids_ext.key, &expected_link_ids_pda,
            "Invalid ResourceExtension PDA for LinkIds"
        );

        // Allocate tunnel_net from global DeviceTunnelBlock (skip if already allocated)
        if link.tunnel_net == NetworkV4::default() {
            let mut buffer = device_tunnel_block_ext.data.borrow_mut();
            let mut resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;
            link.tunnel_net = resource
                .allocate(2)?
                .as_ip()
                .ok_or(DoubleZeroError::InvalidArgument)?;
        }

        // Allocate tunnel_id from global LinkIds (skip if already allocated)
        if link.tunnel_id == 0 {
            let mut buffer = link_ids_ext.data.borrow_mut();
            let mut resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;
            link.tunnel_id = resource
                .allocate(1)?
                .as_id()
                .ok_or(DoubleZeroError::InvalidArgument)?;
        }
    } else {
        // Legacy behavior: use provided args
        link.tunnel_id = value.tunnel_id;
        link.tunnel_net = value.tunnel_net;
    }

    let mut updated_iface_a = side_a_iface.clone();
    updated_iface_a.status = InterfaceStatus::Activated;
    updated_iface_a.ip_net =
        NetworkV4::new(link.tunnel_net.nth(0).unwrap(), link.tunnel_net.prefix()).unwrap();
    side_a_dev.interfaces[idx_a] = updated_iface_a.to_interface();

    let mut updated_iface_z = side_z_iface.clone();
    updated_iface_z.status = InterfaceStatus::Activated;
    updated_iface_z.ip_net =
        NetworkV4::new(link.tunnel_net.nth(1).unwrap(), link.tunnel_net.prefix()).unwrap();
    side_z_dev.interfaces[idx_z] = updated_iface_z.to_interface();

    //TODO: This should be changed once the Health Oracle is finalized.
    //link.status = LinkStatus::Provisioning;
    link.status = LinkStatus::Activated;

    link.check_status_transition();

    try_acc_write(&side_a_dev, side_a_device_account, payer_account, accounts)?;
    try_acc_write(&side_z_dev, side_z_device_account, payer_account, accounts)?;
    try_acc_write(&link, link_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Activated: {:?}", link);

    Ok(())
}
