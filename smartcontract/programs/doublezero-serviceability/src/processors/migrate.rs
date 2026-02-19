use crate::{
    error::DoubleZeroError,
    pda::get_user_pda,
    seeds::{SEED_PREFIX, SEED_USER},
    serializer::{try_acc_close, try_acc_create},
    state::{accounttype::AccountType, user::User},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct MigrateArgs {}

impl fmt::Debug for MigrateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_migrate(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &MigrateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let actual_account = next_account_info(accounts_iter)?;
    let new_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_migrate({:?})", _value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        actual_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(actual_account.is_writable, "PDA Account is not writable");
    assert!(new_account.is_writable, "New Account is not writable");

    assert_ne!(
        actual_account.key, new_account.key,
        "Actual Account and New Account cannot be the same"
    );

    let account_type = AccountType::from(actual_account.data.borrow()[0]);
    if account_type == AccountType::User {
        let mut actual_user = User::try_from(actual_account)?;

        msg!(
            "Migrating User Account from PDA: {} to new PDA: {}",
            actual_account.key,
            new_account.key
        );

        let (new_pubkey, bump_seed) =
            get_user_pda(program_id, &actual_user.client_ip, actual_user.user_type);

        assert_eq!(
            new_account.key, &new_pubkey,
            "New Account PDA does not match expected PDA"
        );

        actual_user.index = 0; // Reset index for new account
        actual_user.bump_seed = bump_seed;
        // Create new account with the same data
        try_acc_create(
            &actual_user,
            new_account,
            payer_account,
            system_program,
            program_id,
            &[
                SEED_PREFIX,
                SEED_USER,
                &actual_user.client_ip.octets(),
                &[actual_user.user_type as u8],
                &[bump_seed],
            ],
        )?;

        msg!("{:?}", actual_user);

        // Close actual account
        try_acc_close(actual_account, payer_account)?;
    } else {
        return Err(DoubleZeroError::InvalidAccountType.into());
    }

    Ok(())
}
