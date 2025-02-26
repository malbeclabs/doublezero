use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use crate::{helper::*, state::tunnel::*};
use crate::pda::*;
use crate::types::*;
#[cfg(test)]
use solana_program::msg;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq)]
pub struct TunnelActivateArgs {
    pub index: u128,
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4, 
}

pub fn process_activate_tunnel(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &TunnelActivateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let config_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_activate_tunnel({:?})", value);

    let (expected_pda_account, bump_seed) = get_tunnel_pda(program_id, value.index);
    assert_eq!(pda_account.key, &expected_pda_account, "Invalid Tunnel PubKey");

    if pda_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }
    if config_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let mut tunnel: Tunnel = Tunnel::from(&pda_account.try_borrow_data().unwrap()[..]);
    if tunnel.status != TunnelStatus::Pending {
        return Err(solana_program::program_error::ProgramError::Custom(1));
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
