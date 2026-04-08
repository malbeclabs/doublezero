use crate::{
    error::DoubleZeroError,
    pda::{get_index_pda, get_resource_extension_pda},
    processors::{resource::deallocate_ip, validation::validate_program_account},
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

    // Account layout WITH deallocation:
    //   [multicastgroup, owner, globalstate, multicast_group_block, index, payer, system]
    // Account layout WITHOUT deallocation:
    //   [multicastgroup, owner, globalstate, index, payer, system]
    let resource_extension_account = if value.use_onchain_deallocation {
        let multicast_group_block_ext = next_account_info(accounts_iter)?;
        Some(multicast_group_block_ext)
    } else {
        None
    };

    let index_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_deactivate_multicastgroup({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate accounts
    validate_program_account!(
        multicastgroup_account,
        program_id,
        writable = true,
        "MulticastGroup"
    );
    validate_program_account!(
        globalstate_account,
        program_id,
        writable = false,
        "GlobalState"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
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
        let (expected_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::MulticastGroupBlock);
        validate_program_account!(
            multicast_group_block_ext,
            program_id,
            writable = true,
            pda = &expected_pda,
            "MulticastGroupBlock"
        );

        // Deallocate multicast_ip from global MulticastGroupBlock
        deallocate_ip(
            multicast_group_block_ext,
            multicastgroup.multicast_ip.into(),
        );
    }

    try_acc_close(multicastgroup_account, owner_account)?;

    // Close the Index account (skip for pre-migration accounts using Pubkey::default())
    if *index_account.key != Pubkey::default() {
        let (expected_index_pda, _) =
            get_index_pda(program_id, SEED_MULTICAST_GROUP, &multicastgroup.code);
        validate_program_account!(
            index_account,
            program_id,
            writable = true,
            pda = &expected_index_pda,
            "Index"
        );

        let index = Index::try_from(index_account)?;
        assert_eq!(
            index.pk, *multicastgroup_account.key,
            "Index does not point to this MulticastGroup"
        );

        try_acc_close(index_account, payer_account)?;
    }

    #[cfg(test)]
    msg!("Deactivated: MulticastGroup closed");

    Ok(())
}
