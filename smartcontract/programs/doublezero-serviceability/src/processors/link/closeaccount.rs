use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    processors::{
        resource::{deallocate_id, deallocate_ip},
        validation::validate_program_account,
    },
    resource::ResourceType,
    serializer::{try_acc_close, try_acc_write},
    state::{
        contributor::Contributor,
        device::*,
        globalstate::GlobalState,
        interface::{InterfaceCYOA, InterfaceDIA, InterfaceStatus, InterfaceType},
        link::*,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::types::NetworkV4;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::fmt;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct LinkCloseAccountArgs {
    /// When true, on-chain deallocation is used (ResourceExtension accounts required).
    /// When false, legacy behavior is used (no deallocation).
    #[incremental(default = false)]
    pub use_onchain_deallocation: bool,
}

impl fmt::Debug for LinkCloseAccountArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "use_onchain_deallocation: {}",
            self.use_onchain_deallocation
        )
    }
}

pub fn process_closeaccount_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkCloseAccountArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let link_account = next_account_info(accounts_iter)?;
    let owner_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let side_a_account = next_account_info(accounts_iter)?;
    let side_z_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Optional: ResourceExtension accounts for on-chain deallocation (before payer)
    // Account layout WITH ResourceExtension (use_onchain_deallocation = true):
    //   [link, owner, contributor, side_a, side_z, globalstate, device_tunnel_block, link_ids, payer, system]
    // Account layout WITHOUT (legacy, use_onchain_deallocation = false):
    //   [link, owner, contributor, side_a, side_z, globalstate, payer, system]
    let resource_extension_accounts = if value.use_onchain_deallocation {
        let device_tunnel_block_ext = next_account_info(accounts_iter)?; // DeviceTunnelBlock (global)
        let link_ids_ext = next_account_info(accounts_iter)?; // LinkIds (global)
        Some((device_tunnel_block_ext, link_ids_ext))
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_closeaccount_link({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate accounts
    validate_program_account!(
        link_account,
        program_id,
        writable = true,
        pda = None::<&Pubkey>,
        "Link"
    );
    validate_program_account!(
        contributor_account,
        program_id,
        writable = false,
        pda = None::<&Pubkey>,
        "Contributor"
    );
    validate_program_account!(
        side_a_account,
        program_id,
        writable = false,
        pda = None::<&Pubkey>,
        "SideA"
    );
    validate_program_account!(
        side_z_account,
        program_id,
        writable = false,
        pda = None::<&Pubkey>,
        "SideZ"
    );
    validate_program_account!(
        globalstate_account,
        program_id,
        writable = false,
        pda = None::<&Pubkey>,
        "GlobalState"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    let globalstate = GlobalState::try_from(globalstate_account)?;

    // Authorization: allow activator_authority_pk OR foundation_allowlist (matching ActivateLink)
    let is_activator = globalstate.activator_authority_pk == *payer_account.key;
    let is_foundation = globalstate.foundation_allowlist.contains(payer_account.key);
    if !is_activator && !is_foundation {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut contributor = Contributor::try_from(contributor_account)?;
    let mut side_a_dev = Device::try_from(side_a_account)?;
    let mut side_z_dev = Device::try_from(side_z_account)?;
    let link: Link = Link::try_from(link_account)?;
    if link.contributor_pk != *contributor_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if link.owner != *owner_account.key
        || link.side_a_pk != *side_a_account.key
        || link.side_z_pk != *side_z_account.key
    {
        return Err(ProgramError::InvalidAccountData);
    }
    if link.status != LinkStatus::Deleting {
        #[cfg(test)]
        msg!("{:?}", link);
        return Err(solana_program::program_error::ProgramError::Custom(1));
    }

    // Deallocate resources from ResourceExtension if accounts provided
    // Deallocation is idempotent - safe to call even if resources weren't allocated
    if let Some((device_tunnel_block_ext, link_ids_ext)) = resource_extension_accounts {
        // Validate DeviceTunnelBlock
        let (expected_device_tunnel_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::DeviceTunnelBlock);
        validate_program_account!(
            device_tunnel_block_ext,
            program_id,
            writable = true,
            pda = Some(&expected_device_tunnel_pda),
            "DeviceTunnelBlock"
        );

        // Validate LinkIds
        let (expected_link_ids_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::LinkIds);
        validate_program_account!(
            link_ids_ext,
            program_id,
            writable = true,
            pda = Some(&expected_link_ids_pda),
            "LinkIds"
        );

        // Deallocate tunnel_net from global DeviceTunnelBlock
        deallocate_ip(device_tunnel_block_ext, link.tunnel_net);

        // Deallocate tunnel_id from global LinkIds
        deallocate_id(link_ids_ext, link.tunnel_id);
    }

    if let Ok((idx_a, side_a_iface)) = side_a_dev.find_interface(&link.side_a_iface_name) {
        let mut updated_iface = side_a_iface.clone();
        updated_iface.status = InterfaceStatus::Unlinked;
        // Preserve user-provided ip_net for CYOA/DIA physical interfaces.
        // For all other interfaces (loopbacks, plain physical), reset ip_net since it was
        // set from tunnel_net during activation and is no longer valid.
        let has_user_ip = updated_iface.interface_type == InterfaceType::Physical
            && (updated_iface.interface_cyoa != InterfaceCYOA::None
                || updated_iface.interface_dia != InterfaceDIA::None);
        if !has_user_ip {
            updated_iface.ip_net = NetworkV4::default();
        }
        side_a_dev.interfaces[idx_a] = updated_iface.to_interface();
    }

    if let Ok((idx_z, side_z_iface)) = side_z_dev.find_interface(&link.side_z_iface_name) {
        let mut updated_iface = side_z_iface.clone();
        updated_iface.status = InterfaceStatus::Unlinked;
        // Preserve user-provided ip_net for CYOA/DIA physical interfaces.
        // For all other interfaces (loopbacks, plain physical), reset ip_net since it was
        // set from tunnel_net during activation and is no longer valid.
        let has_user_ip = updated_iface.interface_type == InterfaceType::Physical
            && (updated_iface.interface_cyoa != InterfaceCYOA::None
                || updated_iface.interface_dia != InterfaceDIA::None);
        if !has_user_ip {
            updated_iface.ip_net = NetworkV4::default();
        }
        side_z_dev.interfaces[idx_z] = updated_iface.to_interface();
    }

    contributor.reference_count = contributor.reference_count.saturating_sub(1);
    side_a_dev.reference_count = side_a_dev.reference_count.saturating_sub(1);
    side_z_dev.reference_count = side_z_dev.reference_count.saturating_sub(1);

    try_acc_write(&contributor, contributor_account, payer_account, accounts)?;
    try_acc_write(&side_a_dev, side_a_account, payer_account, accounts)?;
    try_acc_write(&side_z_dev, side_z_account, payer_account, accounts)?;
    try_acc_close(link_account, owner_account)?;

    #[cfg(test)]
    msg!("CloseAccount: Link closed");

    Ok(())
}
