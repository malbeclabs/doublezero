use crate::helper::*;
use crate::pda::*;
use crate::state::exchange::Exchange;
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
#[cfg(test)]
use solana_program::msg;


#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq)]
pub struct ExchangeDeleteArgs {
    pub index: u128,
}

pub fn process_delete_exchange(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &ExchangeDeleteArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_delete_exchange({:?})", value);

    // Check the owner of the accounts
    assert_eq!(pda_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(globalstate_account.owner, program_id, "Invalid GlobalState Account Owner");
    assert_eq!(*system_program.unsigned_key(), solana_program::system_program::id(), "Invalid System Program Account Owner");
    // Check if the account is writable
    assert!(pda_account.is_writable, "PDA Account is not writable");
    
    let (expected_pda_account, _bump_seed) = get_exchange_pda(program_id, value.index);
    assert_eq!(
        pda_account.key, &expected_pda_account,
        "Invalid Exchange PubKey"
    );

    let exchange: Exchange = Exchange::from(&pda_account.try_borrow_data().unwrap()[..]);
    if exchange.owner != *payer_account.key {
        #[cfg(test)]
        msg!("{:?}", exchange);
        return Err(solana_program::program_error::ProgramError::IncorrectProgramId);
    }

    account_close(pda_account, payer_account)?;

    #[cfg(test)]
    msg!("Deleted: {:?}", pda_account);

    Ok(())
}
