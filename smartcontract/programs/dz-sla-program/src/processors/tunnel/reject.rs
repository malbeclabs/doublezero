use std::fmt;

use crate::error::DoubleZeroError;
use crate::helper::*;
use crate::pda::*;
use crate::state::tunnel::*;

use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct TunnelRejectArgs {
    pub index: u128,
    pub error: String,
}

impl fmt::Debug for TunnelRejectArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "error: {}", self.error)
    }
}

pub fn process_reject_tunnel(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &TunnelRejectArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_activate_tunnel({:?})", value);

    let (expected_pda_account, bump_seed) = get_tunnel_pda(program_id, value.index);
    assert_eq!(pda_account.key, &expected_pda_account, "Invalid Device PubKey");

    if pda_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }        
    if globalstate_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }
    let globalstate = globalstate_get_next(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }  

    let mut tunnel: Tunnel = Tunnel::from(&pda_account.try_borrow_data().unwrap()[..]);
    if tunnel.status != TunnelStatus::Pending {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    tunnel.tunnel_id = 0;
    tunnel.tunnel_net = ([0,0,0,0], 0);
    tunnel.status = TunnelStatus::Rejected;
    msg!("Error: {:?}", value.error);

    account_write(
        pda_account,
        &tunnel,
        payer_account,
        system_program,
        bump_seed,
    );

    #[cfg(test)]
    msg!("Rejectd: {:?}", tunnel);

    Ok(())
}

