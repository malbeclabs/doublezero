use core::fmt;
use crate::{error::DoubleZeroError, helper::*, state::tunnel::*};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct TunnelSuspendArgs {
    pub index: u128,
    pub bump_seed: u8,
}

impl fmt::Debug for TunnelSuspendArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
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

        // Check the owner of the accounts
        assert_eq!(pda_account.owner, program_id, "Invalid PDA Account Owner");
        assert_eq!(
            *system_program.unsigned_key(),
            solana_program::system_program::id(),
            "Invalid System Program Account Owner"
        );
        // Check if the account is writable
        assert!(pda_account.is_writable, "PDA Account is not writable");

    let mut tunnel: Tunnel = Tunnel::from(&pda_account.try_borrow_data().unwrap()[..]);
    assert_eq!(tunnel.index, value.index, "Invalid PDA Account Index");
    assert_eq!(tunnel.bump_seed, value.bump_seed, "Invalid PDA Account Bump Seed");

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
    );

    msg!("Suspended: {:?}", tunnel);

    Ok(())
}
