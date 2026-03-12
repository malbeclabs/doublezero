use crate::{
    error::DoubleZeroError,
    pda::{get_index_pda, get_resource_extension_pda},
    processors::resource::deallocate_ip,
    resource::ResourceType,
    seeds::SEED_MULTICAST_GROUP,
    serializer::try_acc_close,
    state::{globalstate::GlobalState, index::Index, multicastgroup::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
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
pub struct MulticastGroupDeactivateArgs {
    /// When true, on-chain deallocation is used (ResourceExtension accounts required).
    /// When false, legacy behavior is used (no deallocation).
    #[incremental(default = false)]
    pub use_onchain_deallocation: bool,
}

impl fmt::Debug for MulticastGroupDeactivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "use_onchain_deallocation: {}",
            self.use_onchain_deallocation
        )
    }
}

pub fn process_closeaccount_multicastgroup(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &MulticastGroupDeactivateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let multicastgroup_account = next_account_info(accounts_iter)?;
    let owner_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Optional: ResourceExtension account for on-chain deallocation (before payer)
    // Account layout WITH ResourceExtension (use_onchain_deallocation = true):
    //   [multicastgroup, owner, globalstate, multicast_group_block, payer, system]
    // Account layout WITHOUT (legacy, use_onchain_deallocation = false):
    //   [multicastgroup, owner, globalstate, payer, system]
    let resource_extension_account = if value.use_onchain_deallocation {
        let multicast_group_block_ext = next_account_info(accounts_iter)?;
        Some(multicast_group_block_ext)
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    // Optional: Index account to close alongside the multicast group
    let index_account = next_account_info(accounts_iter).ok();

    #[cfg(test)]
    msg!("process_deactivate_multicastgroup({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        multicastgroup_account.owner, program_id,
        "Invalid PDA Account Owner"
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
    // Check if the account is writable
    assert!(
        multicastgroup_account.is_writable,
        "PDA Account is not writable"
    );

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if globalstate.activator_authority_pk != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let multicastgroup = MulticastGroup::try_from(multicastgroup_account)?;

    if multicastgroup.owner != *owner_account.key {
        return Err(ProgramError::InvalidAccountData);
    }
    if multicastgroup.status != MulticastGroupStatus::Deleting {
        #[cfg(test)]
        msg!("{:?}", multicastgroup);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    // Deallocate multicast_ip from ResourceExtension if account provided
    // Deallocation is idempotent - safe to call even if resource wasn't allocated
    if let Some(multicast_group_block_ext) = resource_extension_account {
        // Validate multicast_group_block_ext (MulticastGroupBlock - global)
        assert_eq!(
            multicast_group_block_ext.owner, program_id,
            "Invalid ResourceExtension Account Owner for MulticastGroupBlock"
        );
        assert!(
            multicast_group_block_ext.is_writable,
            "ResourceExtension Account for MulticastGroupBlock is not writable"
        );
        assert!(
            !multicast_group_block_ext.data_is_empty(),
            "ResourceExtension Account for MulticastGroupBlock is empty"
        );

        let (expected_multicast_group_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::MulticastGroupBlock);
        assert_eq!(
            multicast_group_block_ext.key, &expected_multicast_group_pda,
            "Invalid ResourceExtension PDA for MulticastGroupBlock"
        );

        // Deallocate multicast_ip from global MulticastGroupBlock
        deallocate_ip(
            multicast_group_block_ext,
            multicastgroup.multicast_ip.into(),
        );
    }

    try_acc_close(multicastgroup_account, owner_account)?;

    // Close the Index account if provided
    if let Some(index_acc) = index_account {
        assert_eq!(index_acc.owner, program_id, "Invalid Index Account Owner");
        assert!(index_acc.is_writable, "Index Account is not writable");

        let (expected_index_pda, _) =
            get_index_pda(program_id, SEED_MULTICAST_GROUP, &multicastgroup.code);
        assert_eq!(index_acc.key, &expected_index_pda, "Invalid Index Pubkey");

        let index = Index::try_from(index_acc)?;
        assert_eq!(
            index.pk, *multicastgroup_account.key,
            "Index does not point to this MulticastGroup"
        );

        try_acc_close(index_acc, payer_account)?;
    }

    #[cfg(test)]
    msg!("Deactivated: MulticastGroup closed");

    Ok(())
}
