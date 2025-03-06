use core::fmt;

use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use crate::{error::DoubleZeroError, helper::*, state::tunnel::*};
use crate::pda::*;
use crate::types::*;
#[cfg(test)]
use solana_program::msg;

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct TunnelActivateArgs {
    pub index: u128,
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4, 
}

impl fmt::Debug for TunnelActivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "tunnel_id: {}, tunnel_net: {}", self.tunnel_id, networkv4_to_string(&self.tunnel_net))
    }
}

pub fn process_activate_tunnel(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &TunnelActivateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_activate_tunnel({:?})", value);

    let (expected_pda_account, bump_seed) = get_tunnel_pda(program_id, value.index);
    assert_eq!(pda_account.key, &expected_pda_account, "Invalid Tunnel PubKey");

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

    tunnel.tunnel_id = value.tunnel_id;
    tunnel.tunnel_net = value.tunnel_net;
    tunnel.status = TunnelStatus::Activated;

    account_write(
        pda_account,
        &tunnel,
        payer_account,
        system_program,
        bump_seed,
    );

    #[cfg(test)]
    msg!("Activated: {:?}", tunnel);

    Ok(())
}
