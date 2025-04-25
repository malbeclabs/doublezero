use core::fmt;

use crate::pda::*;
use crate::{error::DoubleZeroError, helper::*, state::tunnel::*};
use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};
#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct TunnelReactivateArgs {
    pub index: u128,
}

impl fmt::Debug for TunnelReactivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

pub fn process_reactivate_tunnel(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &TunnelReactivateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_reactivate_tunnel({:?})", value);

    let (expected_pda_account, bump_seed) = get_tunnel_pda(program_id, value.index);
    assert_eq!(
        pda_account.key, &expected_pda_account,
        "Invalid Tunnel PubKey"
    );

    if pda_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let mut tunnel: Tunnel = Tunnel::from(&pda_account.try_borrow_data().unwrap()[..]);
    if tunnel.owner != *payer_account.key {
        return Err(solana_program::program_error::ProgramError::Custom(0));
    }

    if tunnel.status != TunnelStatus::Suspended {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    tunnel.status = TunnelStatus::Activated;

    account_write(
        pda_account,
        &tunnel,
        payer_account,
        system_program,
        bump_seed,
    );

    #[cfg(test)]
    msg!("Suspended: {:?}", tunnel);

    Ok(())
}
