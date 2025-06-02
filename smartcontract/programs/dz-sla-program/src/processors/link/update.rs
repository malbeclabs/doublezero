use crate::error::DoubleZeroError;
use crate::globalstate::globalstate_get;
use crate::{helper::*, state::link::*};
use borsh::{BorshDeserialize, BorshSerialize};
use core::fmt;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
#[derive(BorshSerialize, BorshDeserialize, PartialEq, Clone)]
pub struct LinkUpdateArgs {
    pub index: u128,
    pub bump_seed: u8,
    pub code: Option<String>,
    pub tunnel_type: Option<LinkLinkType>,
    pub bandwidth: Option<u64>,
    pub mtu: Option<u32>,
    pub delay_ns: Option<u64>,
    pub jitter_ns: Option<u64>,
}

impl fmt::Debug for LinkUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {:?}, tunnel_type: {:?}, bandwidth: {:?}, mtu: {:?}, delay_ns: {:?}, jitter_ns: {:?}",
            self.code, self.tunnel_type, self.bandwidth, self.mtu, self.delay_ns, self.jitter_ns
        )
    }
}

pub fn process_update_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let pda_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_link({:?})", value);

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

    let mut tunnel: Link = Link::from(&pda_account.try_borrow_data().unwrap()[..]);
    assert_eq!(tunnel.index, value.index, "Invalid PDA Account Index");
    assert_eq!(
        tunnel.bump_seed, value.bump_seed,
        "Invalid PDA Account Bump Seed"
    );

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

    account_write(pda_account, &tunnel, payer_account, system_program);

    #[cfg(test)]
    msg!("Updated: {:?}", tunnel);

    Ok(())
}
