use crate::{
    error::DoubleZeroError,
    pda::{get_multicastgroup_pda, get_resource_extension_pda},
    processors::{resource::allocate_ip, validation::validate_program_account},
    resource::ResourceType,
    seeds::{SEED_MULTICAST_GROUP, SEED_PREFIX},
    serializer::{try_acc_create, try_acc_write},
    state::{accounttype::AccountType, globalstate::GlobalState, multicastgroup::*},
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
    if !value.use_onchain_allocation {
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    let accounts_iter = &mut accounts.iter();

    let mgroup_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Account layout: [mgroup, globalstate, multicast_group_block, payer, system]
    let multicast_group_block_ext = next_account_info(accounts_iter)?;

    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_create_multicastgroup({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Validate and normalize code
    let code =
        validate_account_code(&value.code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;

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

    // Atomic create+allocate+activate.
    let (expected_pda, _, _) =
        get_resource_extension_pda(program_id, ResourceType::MulticastGroupBlock);
    validate_program_account!(
        multicast_group_block_ext,
        program_id,
        writable = true,
        pda = &expected_pda,
        "MulticastGroupBlock"
    );

    let multicastgroup = MulticastGroup {
        account_type: AccountType::MulticastGroup,
        owner: value.owner,
        index: globalstate.account_index,
        bump_seed,
        tenant_pk: Pubkey::default(),
        code,
        multicast_ip: allocate_ip(multicast_group_block_ext, 1)?.ip(),
        max_bandwidth: value.max_bandwidth,
        status: MulticastGroupStatus::Activated,
        publisher_count: 0,
        subscriber_count: 0,
    };

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
    try_acc_write(&globalstate, globalstate_account, payer_account, accounts)?;

    Ok(())
}
