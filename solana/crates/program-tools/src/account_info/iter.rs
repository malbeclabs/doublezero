use std::{iter::Enumerate, slice::Iter};

use solana_account_info::AccountInfo;
use solana_msg::msg;
use solana_program_error::ProgramError;
use solana_pubkey::Pubkey;

pub type EnumeratedAccountInfoIter<'a, 'b> = Enumerate<Iter<'a, AccountInfo<'b>>>;

pub trait TryNextAccounts<'a, 'b: 'a, ExtraArgs>: Sized {
    fn try_next_accounts(
        accounts_iter: &mut EnumeratedAccountInfoIter<'a, 'b>,
        extra_args: ExtraArgs,
    ) -> Result<Self, ProgramError>;
}

#[derive(Debug, Default, PartialEq)]
pub struct NextAccountOptions<'a> {
    pub must_be_signer: bool,
    pub must_be_writable: bool,
    pub owned_by: Option<&'a Pubkey>,
}

#[inline(always)]
pub fn try_next_enumerated_account<'a, 'b>(
    accounts_iter: &mut EnumeratedAccountInfoIter<'a, 'b>,
    opts: NextAccountOptions,
) -> Result<(usize, &'a AccountInfo<'b>), ProgramError> {
    let (index, account_info) = accounts_iter
        .next()
        .ok_or(ProgramError::NotEnoughAccountKeys)?;

    let NextAccountOptions {
        must_be_signer,
        must_be_writable,
        owned_by,
    } = opts;

    if must_be_signer && !account_info.is_signer {
        msg!("Account {} must be signer", index);
        return Err(ProgramError::MissingRequiredSignature);
    }

    if must_be_writable && !account_info.is_writable {
        msg!("Account {} must be writable", index);
        return Err(ProgramError::InvalidAccountData);
    }

    if let Some(expected_owner) = owned_by {
        if account_info.owner != expected_owner {
            msg!(
                "Unexpected owner for account {}. Expected {}, found {}",
                index,
                expected_owner,
                account_info.owner
            );
            return Err(ProgramError::InvalidAccountOwner);
        }
    }

    Ok((index, account_info))
}
