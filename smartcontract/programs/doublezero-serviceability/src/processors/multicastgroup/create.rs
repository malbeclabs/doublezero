use crate::{
    error::DoubleZeroError,
    pda::{get_index_pda, get_multicastgroup_pda, get_resource_extension_pda},
    processors::{resource::allocate_ip, validation::validate_program_account},
    resource::ResourceType,
    seeds::{SEED_INDEX, SEED_MULTICAST_GROUP, SEED_PREFIX},
    serializer::{try_acc_create, try_acc_write},
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
use doublezero_program_common::validate_account_code;
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
pub struct MulticastGroupCreateArgs {
    pub code: String,
    pub max_bandwidth: u64,
    pub owner: Pubkey,
    /// When true, onchain allocation is used (ResourceExtension accounts required).
    /// Performs atomic create+allocate+activate in a single transaction.
    #[incremental(default = false)]
    pub use_onchain_allocation: bool,
}

impl fmt::Debug for MulticastGroupCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {}, max_bandwidth: {}, owner: {}, use_onchain_allocation: {}",
            self.code, self.max_bandwidth, self.owner, self.use_onchain_allocation
        )
    }
}

pub fn process_create_multicastgroup(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &MulticastGroupCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let mgroup_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Optional: ResourceExtension account for onchain allocation
    // Account layout WITH ResourceExtension (use_onchain_allocation = true):
    //   [mgroup, globalstate, multicast_group_block, index, payer, system]
    // Account layout WITHOUT (legacy, use_onchain_allocation = false):
    //   [mgroup, globalstate, index, payer, system]
    let resource_extension_account = if value.use_onchain_allocation {
        Some(next_account_info(accounts_iter)?)
    } else {
        None
    };

    let index_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_multicastgroup({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate and normalize code
    let code =
        validate_account_code(&value.code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;
    let lowercase_code = code.to_ascii_lowercase();

    // Check the owner of the accounts
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
    assert!(mgroup_account.is_writable, "PDA Account is not writable");

    // Parse the global state account & check if the payer is in the allowlist
    let mut globalstate = GlobalState::try_from(globalstate_account)?;
    globalstate.account_index += 1;

    // Get the PDA pubkey and bump seed for the account multicastgroup & check if it matches the account
    let (expected_pda_account, bump_seed) =
        get_multicastgroup_pda(program_id, globalstate.account_index);
    assert_eq!(
        mgroup_account.key, &expected_pda_account,
        "Invalid MulticastGroup Pubkey"
    );
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Check if the account is already initialized
    if !mgroup_account.data_is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    // Validate Index PDA (before code is moved into multicastgroup)
    let (expected_index_pda, index_bump_seed) =
        get_index_pda(program_id, SEED_MULTICAST_GROUP, &code);

    let mut multicastgroup = MulticastGroup {
        account_type: AccountType::MulticastGroup,
        owner: value.owner,
        index: globalstate.account_index,
        bump_seed,
        tenant_pk: Pubkey::default(),
        code,
        multicast_ip: std::net::Ipv4Addr::UNSPECIFIED,
        max_bandwidth: value.max_bandwidth,
        status: MulticastGroupStatus::Pending,
        publisher_count: 0,
        subscriber_count: 0,
    };

    // Atomic create+allocate+activate if onchain allocation is enabled
    if let Some(multicast_group_block_ext) = resource_extension_account {
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

        multicastgroup.multicast_ip = allocate_ip(multicast_group_block_ext, 1)?.ip();
        multicastgroup.status = MulticastGroupStatus::Activated;
    }
    assert_eq!(
        index_account.key, &expected_index_pda,
        "Invalid Index Pubkey"
    );
    assert!(index_account.is_writable, "Index Account is not writable");

    // Uniqueness: index account must not already exist
    if !index_account.data_is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    try_acc_create(
        &multicastgroup,
        mgroup_account,
        payer_account,
        system_program,
        program_id,
        &[
            SEED_PREFIX,
            SEED_MULTICAST_GROUP,
            &globalstate.account_index.to_le_bytes(),
            &[bump_seed],
        ],
    )?;

    // Create the Index account pointing to the multicast group
    let index = Index {
        account_type: AccountType::Index,
        pk: *mgroup_account.key,
        bump_seed: index_bump_seed,
    };

    try_acc_create(
        &index,
        index_account,
        payer_account,
        system_program,
        program_id,
        &[
            SEED_PREFIX,
            SEED_INDEX,
            SEED_MULTICAST_GROUP,
            lowercase_code.as_bytes(),
            &[index_bump_seed],
        ],
    )?;

    try_acc_write(&globalstate, globalstate_account, payer_account, accounts)?;

    Ok(())
}
