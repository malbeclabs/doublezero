use crate::error::DoubleZeroError;
use crate::globalstate::globalstate_get;
use crate::helper::*;
use crate::state::user::*;
use crate::types::*;
use core::fmt;

use borsh::{BorshDeserialize, BorshSerialize};
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct UserActivateArgs {
    pub index: u128,
    pub bump_seed: u8,
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
    pub dz_ip: IpV4,
}

impl fmt::Debug for UserActivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "tunnel_id: {}, tunnel_net: {}, dz_ip: {}",
            self.tunnel_id,
            networkv4_to_string(&self.tunnel_net),
            ipv4_to_string(&self.dz_ip)
        )
    }
}

pub fn process_activate_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserActivateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_activate_user({:?})", value);

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
    // Check if the account is writable
    assert!(pda_account.is_writable, "PDA Account is not writable");

    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut user: User = User::from(&pda_account.try_borrow_data().unwrap()[..]);
    assert_eq!(user.index, value.index, "Invalid PDA Account Index");
    assert_eq!(
        user.bump_seed, value.bump_seed,
        "Invalid PDA Account Bump Seed"
    );
    if user.status != UserStatus::Pending {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    user.tunnel_id = value.tunnel_id;
    user.tunnel_net = value.tunnel_net;
    user.dz_ip = value.dz_ip;
    user.status = UserStatus::Activated;

    account_write(pda_account, &user, payer_account, system_program);

    #[cfg(test)]
    msg!("Activated: {:?}", user);

    Ok(())
}
