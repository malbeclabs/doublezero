use std::{fmt, str::FromStr};

use crate::helper::account_close;
use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct CloseAccountArgs {
    pub pubkey: Pubkey,
}

impl fmt::Debug for CloseAccountArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "pubkey: {}", self.pubkey)
    }
}

pub fn process_close_account(
    _program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &CloseAccountArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("close_account()");

    let pubkey: Pubkey = Pubkey::from_str("gwfHPG4suqu1aiXEjCPyW9rZfKnb9zQqdNt4iyqiA1D").unwrap();

    assert_eq!(*payer_account.key, pubkey, "Invalid payer account");
    assert_eq!(pda_account.key, &value.pubkey, "Invalid PDA PubKey");

    account_close(pda_account, payer_account)?;

    #[cfg(test)]
    msg!("{:?}", pda_account);

    Ok(())
}
