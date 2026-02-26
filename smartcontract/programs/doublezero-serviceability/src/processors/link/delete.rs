use crate::{
    error::DoubleZeroError,
    serializer::{try_acc_close, try_acc_write},
    state::{
        accounttype::AccountType,
        contributor::Contributor,
        device::*,
        globalstate::GlobalState,
        interface::{InterfaceCYOA, InterfaceDIA, InterfaceStatus, InterfaceType},
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

use super::resource_onchain_helpers;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct LinkDeleteArgs {
    /// When true, atomic delete+deallocate+close in a single transaction.
    /// Requires ResourceExtension accounts and additional device/owner accounts.
    #[incremental(default = false)]
    pub use_onchain_deallocation: bool,
}

impl fmt::Debug for LinkDeleteArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "use_onchain_deallocation: {}",
            self.use_onchain_deallocation
        )
    }
}

pub fn process_delete_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkDeleteArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let link_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Optional: additional accounts for atomic deallocation (before payer)
    // Account layout WITH deallocation (use_onchain_deallocation = true):
    //   [link, contributor, globalstate, side_a, side_z, device_tunnel_block, link_ids, owner, payer, system]
    // Account layout WITHOUT (legacy, use_onchain_deallocation = false):
    //   [link, contributor, globalstate, payer, system]
    let deallocation_accounts = if value.use_onchain_deallocation {
        let side_a_account = next_account_info(accounts_iter)?;
        let side_z_account = next_account_info(accounts_iter)?;
        let device_tunnel_block_ext = next_account_info(accounts_iter)?;
        let link_ids_ext = next_account_info(accounts_iter)?;
        let owner_account = next_account_info(accounts_iter)?;
        Some((
            side_a_account,
            side_z_account,
            device_tunnel_block_ext,
            link_ids_ext,
            owner_account,
        ))
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_delete_link({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(link_account.owner, program_id, "Invalid PDA Account Owner");
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

    let globalstate = GlobalState::try_from(globalstate_account)?;
    assert_eq!(globalstate.account_type, AccountType::GlobalState);

    let mut contributor = Contributor::try_from(contributor_account)?;

    let payer_in_foundation = globalstate.foundation_allowlist.contains(payer_account.key);

    if contributor.owner != *payer_account.key && !payer_in_foundation {
        return Err(DoubleZeroError::InvalidOwnerPubkey.into());
    }

    // Any link can be deleted by its contributor or foundation allowlist
    let link: Link = Link::try_from(link_account)?;

    // Status check differs between legacy and atomic paths
    if value.use_onchain_deallocation {
        // Atomic: reject only Deleting (already being deleted)
        // Allow Activated/SoftDrained/HardDrained — we deallocate and close in one step
        if link.status == LinkStatus::Deleting {
            return Err(DoubleZeroError::InvalidStatus.into());
        }
    } else {
        // Legacy: reject Activated and Deleting
        if matches!(link.status, LinkStatus::Activated | LinkStatus::Deleting) {
            return Err(DoubleZeroError::InvalidStatus.into());
        }
    }

    if !payer_in_foundation && link.contributor_pk != *contributor_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if let Some((
        side_a_account,
        side_z_account,
        device_tunnel_block_ext,
        link_ids_ext,
        owner_account,
    )) = deallocation_accounts
    {
        // Validate additional account owners
        assert_eq!(
            side_a_account.owner, program_id,
            "Invalid Side A Account Owner"
        );
        assert_eq!(
            side_z_account.owner, program_id,
            "Invalid Side Z Account Owner"
        );

        // Validate link references match accounts
        if link.owner != *owner_account.key
            || link.side_a_pk != *side_a_account.key
            || link.side_z_pk != *side_z_account.key
        {
            return Err(ProgramError::InvalidAccountData);
        }

        // Deallocate resources via helper (checks feature flag, validates PDAs)
        resource_onchain_helpers::validate_and_deallocate_link_resources(
            program_id,
            &link,
            device_tunnel_block_ext,
            link_ids_ext,
            &globalstate,
        )?;

        // Reset interfaces to Unlinked
        let mut side_a_dev = Device::try_from(side_a_account)?;
        let mut side_z_dev = Device::try_from(side_z_account)?;

        if let Ok((idx_a, side_a_iface)) = side_a_dev.find_interface(&link.side_a_iface_name) {
            let mut updated_iface = side_a_iface.clone();
            updated_iface.status = InterfaceStatus::Unlinked;
            // Preserve user-provided ip_net for CYOA/DIA physical interfaces.
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
            let has_user_ip = updated_iface.interface_type == InterfaceType::Physical
                && (updated_iface.interface_cyoa != InterfaceCYOA::None
                    || updated_iface.interface_dia != InterfaceDIA::None);
            if !has_user_ip {
                updated_iface.ip_net = NetworkV4::default();
            }
            side_z_dev.interfaces[idx_z] = updated_iface.to_interface();
        }

        // Decrement reference counts
        contributor.reference_count = contributor.reference_count.saturating_sub(1);
        side_a_dev.reference_count = side_a_dev.reference_count.saturating_sub(1);
        side_z_dev.reference_count = side_z_dev.reference_count.saturating_sub(1);

        try_acc_write(&contributor, contributor_account, payer_account, accounts)?;
        try_acc_write(&side_a_dev, side_a_account, payer_account, accounts)?;
        try_acc_write(&side_z_dev, side_z_account, payer_account, accounts)?;
        try_acc_close(link_account, owner_account)?;

        #[cfg(test)]
        msg!("DeleteLink (atomic): Link deallocated and closed");
    } else {
        // Legacy path: just mark as Deleting
        let mut link: Link = Link::try_from(link_account)?;
        link.status = LinkStatus::Deleting;

        try_acc_write(&link, link_account, payer_account, accounts)?;

        #[cfg(test)]
        msg!("Deleting: {:?}", link);
    }

    Ok(())
}
