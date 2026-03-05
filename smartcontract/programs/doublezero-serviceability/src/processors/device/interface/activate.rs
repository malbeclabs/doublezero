use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    processors::resource::{allocate_id, allocate_ip},
    resource::ResourceType,
    serializer::try_acc_write,
    state::{
        device::*,
        globalstate::GlobalState,
        interface::{InterfaceStatus, InterfaceType, LoopbackType},
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
pub struct DeviceInterfaceActivateArgs {
    pub name: String,
    pub ip_net: NetworkV4,
    pub node_segment_idx: u16,
}

impl fmt::Debug for DeviceInterfaceActivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "{}, ip_net: {}, node_segment_idx: {}",
            self.name, self.ip_net, self.node_segment_idx
        )
    }
}

pub fn process_activate_device_interface(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceInterfaceActivateArgs,
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
    msg!("process_activate_device_interface()");

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    if device_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }
    if globalstate_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

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

    let mut device: Device = Device::try_from(device_account)?;

    let (idx, iface) = device
        .find_interface(&value.name)
        .map_err(|_| DoubleZeroError::InterfaceNotFound)?;

    if iface.status != InterfaceStatus::Pending && iface.status != InterfaceStatus::Unlinked {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    let mut updated_iface = iface.clone();
    updated_iface.status = InterfaceStatus::Activated;
    if let (Some(link_ips_acc), Some(segment_routing_ids_acc)) =
        (link_ips_account, segment_routing_ids_account)
    {
        if updated_iface.interface_type == InterfaceType::Loopback {
            // Allocate ip_net from global DeviceTunnelBlock (skip if already allocated)
            if updated_iface.ip_net == NetworkV4::default() {
                updated_iface.ip_net = allocate_ip(link_ips_acc, 1)?;
            }

            // Allocate segment_routing_id from global LinkIds (skip if already allocated)
            if updated_iface.loopback_type == LoopbackType::Vpnv4
                && updated_iface.node_segment_idx == 0
            {
                updated_iface.node_segment_idx = allocate_id(segment_routing_ids_acc)?;
            }
        }
    } else {
        updated_iface.ip_net = value.ip_net;
        updated_iface.node_segment_idx = value.node_segment_idx;
    }

    device.interfaces[idx] = updated_iface.to_interface();

    try_acc_write(&device, device_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Activated: {:?}", device);

    Ok(())
}
