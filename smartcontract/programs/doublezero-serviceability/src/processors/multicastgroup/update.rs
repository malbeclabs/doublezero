use crate::{
    error::DoubleZeroError,
    pda::{get_index_pda, get_resource_extension_pda},
    processors::{
        resource::{allocate_specific_ip, deallocate_ip},
        validation::validate_program_account,
    },
    resource::ResourceType,
    seeds::{SEED_INDEX, SEED_MULTICAST_GROUP, SEED_PREFIX},
    serializer::{try_acc_close, try_acc_create, try_acc_write},
    state::{
        accounttype::AccountType,
        feature_flags::{is_feature_enabled, FeatureFlag},
        globalstate::GlobalState,
        index::Index,
        multicastgroup::*,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::{types::NetworkV4, validate_account_code};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::fmt;

#[cfg(test)]
use solana_program::msg;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct MulticastGroupUpdateArgs {
    pub code: Option<String>,
    pub multicast_ip: Option<std::net::Ipv4Addr>,
    pub max_bandwidth: Option<u64>,
    pub publisher_count: Option<u32>,
    pub subscriber_count: Option<u32>,
    /// When true, onchain allocation is used for multicast_ip changes.
    /// Requires ResourceExtension account (MulticastGroupBlock).
    #[incremental(default = false)]
    pub use_onchain_allocation: bool,
    /// When true, old and new Index accounts are included for an Index rename.
    /// Set to false when the code change doesn't affect the Index PDA (e.g. case-only rename).
    #[incremental(default = false)]
    pub rename_index: bool,
}

impl fmt::Debug for MulticastGroupUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {:?}, multicast_ip: {:?}, max_bandwidth: {:?}, publisher_count: {:?}, subscriber_count: {:?}, use_onchain_allocation: {}, rename_index: {}",
            self.code, self.multicast_ip, self.max_bandwidth, self.publisher_count, self.subscriber_count, self.use_onchain_allocation, self.rename_index
        )
    }
}

pub fn process_update_multicastgroup(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &MulticastGroupUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let multicastgroup_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Optional: ResourceExtension account for onchain allocation
    // Account layout WITH allocation (use_onchain_allocation = true):
    //   [mgroup, globalstate, multicast_group_block, (opt old_index, new_index), payer, system]
    // Account layout WITHOUT (legacy, use_onchain_allocation = false):
    //   [mgroup, globalstate, (opt old_index, new_index), payer, system]
    let resource_extension_account = if value.use_onchain_allocation {
        Some(next_account_info(accounts_iter)?)
    } else {
        None
    };

    // Optional: Index accounts for code rename (before payer/system)
    let index_accounts = if value.rename_index {
        let old_index_account = next_account_info(accounts_iter)?;
        let new_index_account = next_account_info(accounts_iter)?;
        Some((old_index_account, new_index_account))
    } else {
        None
    };

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_multicastgroup({:?})", value);

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
    // Parse the global state account & check if the payer is in the allowlist
    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Parse the multicastgroup account
    let mut multicastgroup: MulticastGroup = MulticastGroup::try_from(multicastgroup_account)?;

    if let Some(ref code) = value.code {
        let new_code =
            validate_account_code(code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;

        // Rename the Index if accounts are provided (skip for case-only renames
        // where the lowercased PDA is unchanged)
        if let Some((old_index_account, new_index_account)) = index_accounts {
            let new_lowercase_code = new_code.to_ascii_lowercase();

            // Validate old Index PDA
            let (expected_old_index_pda, _) =
                get_index_pda(program_id, SEED_MULTICAST_GROUP, &multicastgroup.code);
            validate_program_account!(
                old_index_account,
                program_id,
                writable = true,
                pda = &expected_old_index_pda,
                "Old Index"
            );

            // Validate new Index PDA
            let (expected_new_index_pda, new_index_bump_seed) =
                get_index_pda(program_id, SEED_MULTICAST_GROUP, &new_code);
            assert_eq!(
                new_index_account.key, &expected_new_index_pda,
                "Invalid new Index Pubkey"
            );
            assert!(
                new_index_account.is_writable,
                "New Index Account is not writable"
            );

            // New index must not already exist (uniqueness)
            if !new_index_account.data_is_empty() {
                return Err(ProgramError::AccountAlreadyInitialized);
            }

            // Verify old index points to this multicast group
            let old_index = Index::try_from(old_index_account)?;
            assert_eq!(
                old_index.pk, *multicastgroup_account.key,
                "Old Index does not point to this MulticastGroup"
            );

            // Create new Index
            let new_index = Index {
                account_type: AccountType::Index,
                pk: *multicastgroup_account.key,
                entity_account_type: AccountType::MulticastGroup,
                key: new_code.clone(),
                bump_seed: new_index_bump_seed,
            };

            try_acc_create(
                &new_index,
                new_index_account,
                payer_account,
                system_program,
                program_id,
                &[
                    SEED_PREFIX,
                    SEED_INDEX,
                    SEED_MULTICAST_GROUP,
                    new_lowercase_code.as_bytes(),
                    &[new_index_bump_seed],
                ],
            )?;

            // Close old Index
            try_acc_close(old_index_account, payer_account)?;
        }

        multicastgroup.code = new_code;
    }
    if let Some(ref multicast_ip) = value.multicast_ip {
        // Handle onchain allocation for IP changes
        if let Some(multicast_group_block_ext) = resource_extension_account.as_ref() {
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

            // Deallocate the old IP (if it was allocated)
            if multicastgroup.multicast_ip != std::net::Ipv4Addr::UNSPECIFIED {
                deallocate_ip(
                    multicast_group_block_ext,
                    multicastgroup.multicast_ip.into(),
                );
            }

            // Allocate the new specific IP
            let new_ip_net =
                NetworkV4::new(*multicast_ip, 32).map_err(|_| DoubleZeroError::InvalidArgument)?;
            allocate_specific_ip(multicast_group_block_ext, new_ip_net)?;
        }

        multicastgroup.multicast_ip = *multicast_ip;
    }
    if let Some(ref max_bandwidth) = value.max_bandwidth {
        multicastgroup.max_bandwidth = *max_bandwidth;
    }
    if let Some(ref publisher_count) = value.publisher_count {
        multicastgroup.publisher_count = *publisher_count;
    }
    if let Some(ref subscriber_count) = value.subscriber_count {
        multicastgroup.subscriber_count = *subscriber_count;
    }

    try_acc_write(
        &multicastgroup,
        multicastgroup_account,
        payer_account,
        accounts,
    )?;

    #[cfg(test)]
    msg!("Updated: {:?}", multicastgroup);

    Ok(())
}
