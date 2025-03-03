use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use crate::{error::DoubleZeroError, helper::*, state::tunnel::*};
use crate::pda::*;
#[cfg(test)]
use solana_program::msg;



#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq)]
pub struct TunnelDeactivateArgs {
    pub index: u128,
}

pub fn process_deactivate_tunnel(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &TunnelDeactivateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();
 
    let pda_account = next_account_info(accounts_iter)?;
    let owner_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;
 
    #[cfg(test)]
    msg!("process_deactivate_tunnel({:?})", value);

    let (expected_pda_account, _bump_seed) = get_tunnel_pda(program_id, value.index);
    assert_eq!(pda_account.key, &expected_pda_account, "Invalid Tunnel PubKey");
 
    if globalstate_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }
    let globalstate = globalstate_get_next(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }  

    let tunnel: Tunnel = Tunnel::from(&pda_account.try_borrow_data().unwrap()[..]);
    if tunnel.owner != *owner_account.key {
        return Err(ProgramError::InvalidAccountData);
    }
    if tunnel.status != TunnelStatus::Deleting {
        #[cfg(test)]
        msg!("{:?}", tunnel);
        return Err(solana_program::program_error::ProgramError::Custom(1));
    }

    account_close(pda_account, owner_account)?;

    #[cfg(test)]
    msg!("Deactivated: {:?}", tunnel);
 
    Ok(())
}
