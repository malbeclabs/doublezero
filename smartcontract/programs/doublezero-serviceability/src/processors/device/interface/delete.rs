use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    processors::{
        resource::{deallocate_id, deallocate_ip},
        validation::validate_program_account,
    },
    resource::ResourceType,
    serializer::try_acc_write,
    state::{
        accounttype::AccountType,
        contributor::Contributor,
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
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct DeviceInterfaceDeleteArgs {
    pub name: String,
    /// When true, atomic delete+deallocate in a single transaction.
    /// Requires ResourceExtension accounts.
    #[incremental(default = false)]
    pub use_onchain_deallocation: bool,
}

impl fmt::Debug for DeviceInterfaceDeleteArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "name: {}, use_onchain_deallocation: {}",
            self.name, self.use_onchain_deallocation
        )
    }
}

pub fn process_delete_device_interface(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &DeviceInterfaceDeleteArgs,
) -> ProgramResult {
    if !value.use_onchain_deallocation {
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    let accounts_iter = &mut accounts.iter();

    let device_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Account layout: [device, contributor, globalstate, device_tunnel_block,
    //                  segment_routing_ids, payer, system]
    let device_tunnel_block_ext = next_account_info(accounts_iter)?;
    let segment_routing_ids_ext = next_account_info(accounts_iter)?;

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_delete_device_interface({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

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
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    assert!(device_account.is_writable, "PDA Account is not writable");
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    let globalstate = GlobalState::try_from(globalstate_account)?;
    assert_eq!(globalstate.account_type, AccountType::GlobalState);

    let contributor = Contributor::try_from(contributor_account)?;

    if contributor.owner != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut device: Device = Device::try_from(device_account)?;

    let (idx, _) = device
        .find_interface(&value.name)
        .map_err(|_| DoubleZeroError::InterfaceNotFound)?;
    let iface = device.interfaces[idx].into_current_version();

    if iface.status != InterfaceStatus::Activated && iface.status != InterfaceStatus::Unlinked {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    let (expected_dtb_pda, _, _) =
        get_resource_extension_pda(program_id, ResourceType::DeviceTunnelBlock);
    validate_program_account!(
        device_tunnel_block_ext,
        program_id,
        writable = true,
        pda = &expected_dtb_pda,
        "DeviceTunnelBlock"
    );

    let (expected_sr_pda, _, _) =
        get_resource_extension_pda(program_id, ResourceType::SegmentRoutingIds);
    validate_program_account!(
        segment_routing_ids_ext,
        program_id,
        writable = true,
        pda = &expected_sr_pda,
        "SegmentRoutingIds"
    );

    // Deallocate resources if this is a loopback interface
    if iface.interface_type == InterfaceType::Loopback {
        // Deallocate ip_net if it was allocated
        if iface.ip_net != NetworkV4::default() {
            deallocate_ip(device_tunnel_block_ext, iface.ip_net);
        }

        // Deallocate node_segment_idx if it was allocated (only for Vpnv4 loopbacks)
        if iface.loopback_type == LoopbackType::Vpnv4 && iface.node_segment_idx != 0 {
            deallocate_id(segment_routing_ids_ext, iface.node_segment_idx);
        }
    }

    // Atomic close: remove interface immediately
    device.interfaces.remove(idx);

    #[cfg(test)]
    msg!(
        "DeleteDeviceInterface (atomic): deallocated and removed {}",
        value.name
    );

    try_acc_write(&device, device_account, payer_account, accounts)?;

    Ok(())
}
