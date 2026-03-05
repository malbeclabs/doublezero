use crate::{
    authorize::authorize,
    error::DoubleZeroError,
    pda::get_permission_pda,
    seeds::{SEED_PERMISSION, SEED_PREFIX},
    serializer::try_acc_create,
    state::{
        accounttype::AccountType,
        globalstate::GlobalState,
        permission::{permission_flags, Permission, PermissionStatus},
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct PermissionCreateArgs {
    /// The pubkey for which this Permission PDA is being created.
    pub user_payer: Pubkey,
    /// Bitmask of permission_flags to grant.
    pub permissions: u128,
}

impl fmt::Debug for PermissionCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "user_payer: {}, permissions: {}",
            self.user_payer, self.permissions
        )
    }
}

pub fn process_create_permission(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &PermissionCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let permission_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    assert!(payer_account.is_signer, "Payer must be a signer");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert!(
        permission_account.is_writable,
        "Permission Account is not writable"
    );

    let (expected_pda, bump_seed) = get_permission_pda(program_id, &value.user_payer);
    if permission_account.key != &expected_pda {
        return Err(ProgramError::InvalidArgument);
    }

    if *permission_account.owner != solana_system_interface::program::ID {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    let globalstate = GlobalState::try_from(globalstate_account)?;
    authorize(
        program_id,
        accounts_iter,
        payer_account.key,
        &globalstate,
        permission_flags::PERMISSION_ADMIN,
    )?;

    let permission = Permission {
        account_type: AccountType::Permission,
        owner: *payer_account.key,
        bump_seed,
        status: PermissionStatus::Activated,
        user_payer: value.user_payer,
        permissions: value.permissions,
    };

    // Validate that at least one known flag is set
    if value.permissions == 0 {
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    try_acc_create(
        &permission,
        permission_account,
        payer_account,
        system_program,
        program_id,
        &[
            SEED_PREFIX,
            SEED_PERMISSION,
            value.user_payer.as_ref(),
            &[bump_seed],
        ],
    )?;

    Ok(())
}
