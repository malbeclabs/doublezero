use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use crate::{helper::*, state::tunnel::*};
use crate::pda::*;
#[cfg(test)]
use solana_program::msg;
#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq)]
pub struct TunnelUpdateArgs {
    pub index: u128,
    pub code: Option<String>,
    pub tunnel_type: Option<TunnelTunnelType>, 
    pub bandwidth: Option<u64>, 
    pub mtu: Option<u32>,  
    pub delay_ns: Option<u64>, 
    pub jitter_ns: Option<u64>,
}

pub fn process_update_tunnel(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &TunnelUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();
 
    let pda_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;
 
    #[cfg(test)]
    msg!("process_update_tunnel({:?})", value);

    let (expected_pda_account, bump_seed) = get_tunnel_pda(program_id, value.index);
    assert_eq!(pda_account.key, &expected_pda_account, "Invalid Tunnel PubKey");
 
    if pda_account.owner != program_id {
        return Err(ProgramError::IncorrectProgramId);
    }

    let mut tunnel: Tunnel = Tunnel::from(&pda_account.try_borrow_data().unwrap()[..]);
    if tunnel.owner != *payer_account.key {
        return Err(solana_program::program_error::ProgramError::Custom(0));
    }

    //tunnel.tunnel_type = value.tunnel_type;
    if let Some(code) = &value.code {
        tunnel.code = code.clone();
    }
    if let Some(tunnel_type) = value.tunnel_type {
        tunnel.tunnel_type = tunnel_type;
    }
    if let Some(bandwidth) = value.bandwidth {
        tunnel.bandwidth = bandwidth;
    }
    if let Some(mtu) = value.mtu {
        tunnel.mtu = mtu;
    }
    if let Some(delay_ns) = value.delay_ns {
        tunnel.delay_ns = delay_ns;
    }
    if let Some(jitter_ns) = value.jitter_ns {
        tunnel.jitter_ns = jitter_ns;
    }

    account_write(
        pda_account,
        &tunnel,
        payer_account,
        system_program,
        bump_seed,
    );
 
    #[cfg(test)]
    msg!("Updated: {:?}", tunnel);
 
    Ok(())
}

