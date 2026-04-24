use crate::{
    error::DoubleZeroError,
    pda::get_resource_extension_pda,
    processors::{resource::deallocate_ip, validation::validate_program_account},
    resource::ResourceType,
    serializer::{try_acc_close, try_acc_write},
    state::{
        feature_flags::{is_feature_enabled, FeatureFlag},
        globalstate::GlobalState,
        multicastgroup::*,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct MulticastGroupDeleteArgs {
    /// When true, atomic delete+deallocate+close in a single transaction.
    /// Requires ResourceExtension accounts and owner account.
    #[incremental(default = false)]
    pub use_onchain_deallocation: bool,
}

impl fmt::Debug for MulticastGroupDeleteArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "use_onchain_deallocation: {}",
            self.use_onchain_deallocation
        )
    }
}

pub fn process_delete_multicastgroup(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &MulticastGroupDeleteArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let multicastgroup_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Optional: additional accounts for atomic deallocation (before payer)
    // Account layout WITH deallocation (use_onchain_deallocation = true):
    //   [mgroup, globalstate, multicast_group_block, owner, payer, system]
    // Account layout WITHOUT (legacy, use_onchain_deallocation = false):
    //   [mgroup, globalstate, payer, system]
    let dealfacility_accounts = if value.use_onchain_deallocation {
        let multicast_group_block_ext = next_account_info(accounts_iter)?;
        let owner_account = next_account_info(accounts_iter)?;
        Some((multicast_group_block_ext, owner_account))
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_delete_multicastgroup({:?})", value);

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
    assert!(
        multicastgroup_account.is_writable,
        "PDA Account is not writable"
    );

    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let multicastgroup: MulticastGroup = MulticastGroup::try_from(multicastgroup_account)?;

    if matches!(multicastgroup.status, MulticastGroupStatus::Deleting) {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    if multicastgroup.publisher_count != 0 || multicastgroup.subscriber_count != 0 {
        #[cfg(test)]
        msg!(
            "MulticastGroup has active publishers or subscribers: {:?}",
            multicastgroup
        );
        return Err(DoubleZeroError::MulticastGroupNotEmpty.into());
    }

    if let Some((multicast_group_block_ext, owner_account)) = dealfacility_accounts {
        // Atomic delete+deallocate+close path
        if !is_feature_enabled(globalstate.feature_flags, FeatureFlag::OnChainAllocation) {
            return Err(DoubleZeroError::FeatureNotEnabled.into());
        }

        let (expected_pda, _, _) =
            get_resource_extension_pda(program_id, ResourceType::MulticastGroupBlock);
        validate_program_account!(
            multicast_group_block_ext,
            program_id,
            writable = true,
            pda = &expected_pda,
            "MulticastGroupBlock"
        );

        if multicastgroup.owner != *owner_account.key {
            return Err(ProgramError::InvalidAccountData);
        }

        // Deallocate multicast_ip from ResourceExtension (idempotent)
        deallocate_ip(
            multicast_group_block_ext,
            multicastgroup.multicast_ip.into(),
        );

        try_acc_close(multicastgroup_account, owner_account)?;

        #[cfg(test)]
        msg!("DeleteMulticastGroup (atomic): deallocated and closed");
    } else {
        // Legacy path: just mark as Deleting
        let mut multicastgroup = multicastgroup;
        multicastgroup.status = MulticastGroupStatus::Deleting;

        try_acc_write(
            &multicastgroup,
            multicastgroup_account,
            payer_account,
            accounts,
        )?;

        #[cfg(test)]
        msg!("Deleted: {:?}", multicastgroup_account);
    }

    Ok(())
}
