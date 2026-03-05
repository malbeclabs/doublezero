use crate::{
    authorize::authorize,
    pda::get_permission_pda,
    serializer::try_acc_close,
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
pub struct PermissionDeleteArgs {}

impl fmt::Debug for PermissionDeleteArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "PermissionDeleteArgs")
    }
}

pub fn process_delete_permission(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &PermissionDeleteArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let permission_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    assert!(payer_account.is_signer, "Payer must be a signer");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        permission_account.owner, program_id,
        "Invalid Permission Account Owner"
    );
    assert!(
        permission_account.is_writable,
        "Permission Account is not writable"
    );

    let permission = Permission::try_from(permission_account)?;

    let (expected_pda, _) = get_permission_pda(program_id, &permission.user_payer);
    if permission_account.key != &expected_pda {
        return Err(ProgramError::InvalidArgument);
    }

    let globalstate = GlobalState::try_from(globalstate_account)?;
    authorize(
        program_id,
        accounts_iter,
        payer_account.key,
        &globalstate,
        permission_flags::PERMISSION_ADMIN,
    )?;

    // Close and refund rent to payer
    try_acc_close(permission_account, payer_account)?;

    Ok(())
}
