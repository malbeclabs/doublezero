use crate::{
    authorize::{authorize, can_grant_foundation},
    error::DoubleZeroError,
    pda::get_permission_pda,
    processors::validation::validate_program_account,
    serializer::try_acc_write,
    state::{
        globalstate::GlobalState,
        permission::{permission_flags, Permission},
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
pub struct PermissionUpdateArgs {
    /// Bits to set (OR into the existing permissions bitmask).
    pub add: u128,
    /// Bits to clear (AND-NOT out of the existing permissions bitmask).
    pub remove: u128,
}

impl fmt::Debug for PermissionUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "add: {}, remove: {}", self.add, self.remove)
    }
}

pub fn process_update_permission(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &PermissionUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let permission_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    assert!(payer_account.is_signer, "Payer must be a signer");
    validate_program_account!(
        globalstate_account,
        program_id,
        writable = false,
        "GlobalState"
    );
    validate_program_account!(
        permission_account,
        program_id,
        writable = true,
        "Permission"
    );

    let mut permission = Permission::try_from(permission_account)?;

    let (expected_pda, _) = get_permission_pda(program_id, &permission.user_payer);
    if permission_account.key != &expected_pda {
        return Err(ProgramError::InvalidArgument);
    }

    if value.add & value.remove != 0 {
        return Err(DoubleZeroError::InvalidArgument.into());
    }
    if value.add == 0 && value.remove == 0 {
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    let globalstate = GlobalState::try_from(globalstate_account)?;

    // The SDK appends the caller's own Permission PDA as the trailing account when one
    // exists on-chain. Capture it for both the PERMISSION_ADMIN check and the stricter
    // FOUNDATION-grant check below.
    let caller_permission = accounts_iter.next();

    authorize(
        program_id,
        &mut caller_permission.into_iter(),
        payer_account.key,
        &globalstate,
        permission_flags::PERMISSION_ADMIN,
    )?;

    // Adding FOUNDATION *directly* is gated beyond PERMISSION_ADMIN: only a
    // foundation_allowlist member or an existing FOUNDATION-flag holder may. NOTE this
    // blocks only the direct grant — a plain PERMISSION_ADMIN can still grant itself
    // GLOBALSTATE_ADMIN and then edit foundation_allowlist, so it is not a hard
    // privilege boundary. FOUNDATION is transitional and slated for deprecation in
    // favor of the granular per-flag permissions.
    if value.add & permission_flags::FOUNDATION != 0
        && !can_grant_foundation(
            program_id,
            caller_permission,
            payer_account.key,
            &globalstate,
        )
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    permission.permissions = (permission.permissions | value.add) & !value.remove;
    try_acc_write(&permission, permission_account, payer_account, accounts)?;

    Ok(())
}
