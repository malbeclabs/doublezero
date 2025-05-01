use core::fmt;

use crate::globalstate::globalstate_get;
use crate::types::*;
use crate::{error::DoubleZeroError, helper::*, state::tunnel::*};
use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct TunnelActivateArgs {
    pub index: u128,
    pub bump_seed: u8,
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
}

impl fmt::Debug for TunnelActivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "tunnel_id: {}, tunnel_net: {}",
            self.tunnel_id,
            networkv4_to_string(&self.tunnel_net)
        )
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

    // Check the owner of the accounts
    assert_eq!(pda_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    // Check if the account is writable
    assert!(pda_account.is_writable, "PDA Account is not writable");

    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut tunnel: Tunnel = Tunnel::from(pda_account);
    assert_eq!(tunnel.index, value.index, "Invalid PDA Account Index");
    assert_eq!(
        tunnel.bump_seed, value.bump_seed,
        "Invalid PDA Account Bump Seed"
    );
    if tunnel.status != TunnelStatus::Pending {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    tunnel.tunnel_id = value.tunnel_id;
    tunnel.tunnel_net = value.tunnel_net;
    tunnel.status = TunnelStatus::Activated;

    account_write(pda_account, &tunnel, payer_account, system_program);

    #[cfg(test)]
    msg!("Activated: {:?}", tunnel);

    Ok(())
}
