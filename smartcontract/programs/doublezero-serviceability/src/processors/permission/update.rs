use crate::{
    authorize::authorize,
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
        pda = None::<&Pubkey>,
        "GlobalState"
    );
    validate_program_account!(
        permission_account,
        program_id,
        writable = true,
        pda = None::<&Pubkey>,
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
    authorize(
        program_id,
        accounts_iter,
        payer_account.key,
        &globalstate,
        permission_flags::PERMISSION_ADMIN,
    )?;

    permission.permissions = (permission.permissions | value.add) & !value.remove;
    try_acc_write(&permission, permission_account, payer_account, accounts)?;

    Ok(())
}
