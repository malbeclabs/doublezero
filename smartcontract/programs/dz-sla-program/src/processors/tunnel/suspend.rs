use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use crate::{error::DoubleZeroError, helper::*, state::tunnel::*};
use crate::pda::*;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq)]
pub struct TunnelSuspendArgs {
    pub index: u128,
}

pub fn process_suspend_tunnel(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &TunnelSuspendArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();
 
    let pda_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;
 
    #[cfg(test)]
    msg!("process_suspend_tunnel({:?})", value);

    let (expected_pda_account, bump_seed) = get_tunnel_pda(program_id, value.index);
    assert_eq!(pda_account.key, &expected_pda_account, "Invalid Tunnel PubKey");
 
    if pda_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let mut tunnel: Tunnel = Tunnel::from(&pda_account.try_borrow_data().unwrap()[..]);
    if tunnel.owner != *payer_account.key {
        return Err(ProgramError::InvalidAccountOwner);
    }
    if tunnel.status != TunnelStatus::Activated {
        #[cfg(test)]
        msg!("{:?}", tunnel);
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    tunnel.status = TunnelStatus::Suspended;

    account_write(
        pda_account,
        &tunnel,
        payer_account,
        system_program,
        bump_seed,
    );

    msg!("Suspended: {:?}", tunnel);
 
    Ok(())
}

