use crate::error::DoubleZeroError;
use crate::globalstate::globalstate_get;
use crate::helper::*;
use crate::state::tunnel::*;
use std::fmt;

use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct TunnelRejectArgs {
    pub index: u128,
    pub bump_seed: u8,
    pub reason: String,
}

impl fmt::Debug for TunnelRejectArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "reason: {}", self.reason)
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

    // Check the owner of the accounts
    assert_eq!(pda_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
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

    tunnel.tunnel_id = 0;
    tunnel.tunnel_net = ([0, 0, 0, 0], 0);
    tunnel.status = TunnelStatus::Rejected;
    msg!("Reason: {:?}", value.reason);

    account_write(pda_account, &tunnel, payer_account, system_program);

    #[cfg(test)]
    msg!("Rejectd: {:?}", tunnel);

    Ok(())
}
