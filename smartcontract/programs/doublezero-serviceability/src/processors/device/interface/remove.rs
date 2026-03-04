use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    resource::{IdOrIp, ResourceType},
    serializer::try_acc_write,
    state::{
        device::*,
        globalstate::GlobalState,
        interface::{InterfaceStatus, InterfaceType, LoopbackType},
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
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct DeviceInterfaceRemoveArgs {
    pub name: String,
}

impl fmt::Debug for DeviceInterfaceRemoveArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "name: {}", self.name)
    }
}

pub fn process_remove_device_interface(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceInterfaceRemoveArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let mut link_ips_account = None;
    let mut segment_routing_ids_account = None;
    if accounts.len() > 4 {
        link_ips_account = Some(next_account_info(accounts_iter)?);
        segment_routing_ids_account = Some(next_account_info(accounts_iter)?);
    }
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_remove_device_interface({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        device_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );

    assert!(device_account.is_writable, "PDA Account is not writable");

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if globalstate.activator_authority_pk != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if let Some(link_ips_acc) = link_ips_account {
        let (link_ips_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::DeviceTunnelBlock);
        assert_eq!(
            link_ips_acc.owner, program_id,
            "Link IPs account has incorrect owner"
        );
        assert_eq!(
            *link_ips_acc.key, link_ips_pda,
            "Link IPs account has incorrect PDA"
        );
        assert!(!link_ips_acc.data_is_empty(), "Link IPs account is empty");
        assert!(
            link_ips_acc.is_writable,
            "Link IPs account must be writable"
        );
    }

    if let Some(segment_routing_ids_acc) = segment_routing_ids_account {
        let (segment_routing_ids_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::SegmentRoutingIds);
        assert_eq!(
            segment_routing_ids_acc.owner, program_id,
            "Segment Routing IDs account has incorrect owner"
        );
        assert_eq!(
            *segment_routing_ids_acc.key, segment_routing_ids_pda,
            "Segment Routing IDs account has incorrect PDA"
        );
        assert!(
            !segment_routing_ids_acc.data_is_empty(),
            "Segment Routing IDs account is empty"
        );
        assert!(
            segment_routing_ids_acc.is_writable,
            "Segment Routing IDs account must be writable"
        );
    }

    let mut device = Device::try_from(device_account)?;
    let (idx, iface) = device
        .find_interface(&value.name)
        .map_err(|_| DoubleZeroError::InterfaceNotFound)?;

    if iface.status != InterfaceStatus::Deleting {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    // Deallocate resources if on-chain deallocation is enabled
    if let (Some(link_ips_acc), Some(segment_routing_ids_acc)) =
        (link_ips_account, segment_routing_ids_account)
    {
        if iface.interface_type == InterfaceType::Loopback {
            // Deallocate ip_net if it was allocated
            if iface.ip_net != NetworkV4::default() {
                let mut buffer = link_ips_acc.data.borrow_mut();
                let mut resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;
                resource.deallocate(&IdOrIp::Ip(iface.ip_net));
            }

            // Deallocate node_segment_idx if it was allocated (only for Vpnv4 loopbacks)
            if iface.loopback_type == LoopbackType::Vpnv4 && iface.node_segment_idx != 0 {
                let mut buffer = segment_routing_ids_acc.data.borrow_mut();
                let mut resource = ResourceExtensionBorrowed::inplace_from(&mut buffer[..])?;
                resource.deallocate(&IdOrIp::Id(iface.node_segment_idx));
            }
        }
    }

    device.interfaces.remove(idx);

    try_acc_write(&device, device_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Remove: Device Interface removed");

    Ok(())
}
