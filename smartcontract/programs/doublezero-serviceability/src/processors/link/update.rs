use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    helper::*,
    state::{accounttype::AccountType, link::*},
};
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
    pub code: Option<String>,
    pub contributor_pk: Option<Pubkey>,
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

    let link_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_link({:?})", value);

    // Check the owner of the accounts
    assert_eq!(link_account.owner, program_id, "Invalid PDA Account Owner");
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
    assert!(link_account.is_writable, "PDA Account is not writable");

    let globalstate = globalstate_get(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut link: Link = Link::try_from(link_account)?;
    assert_eq!(link.account_type, AccountType::Link, "Invalid Account Type");

    if link.owner != *payer_account.key {
        return Err(solana_program::program_error::ProgramError::Custom(0));
    }

    //tunnel.tunnel_type = value.tunnel_type;
    if let Some(code) = &value.code {
        link.code = code.clone();
    }
    if let Some(contributor_pk) = value.contributor_pk {
        link.contributor_pk = contributor_pk;
    }
    if let Some(tunnel_type) = value.tunnel_type {
        link.link_type = tunnel_type;
    }
    if let Some(bandwidth) = value.bandwidth {
        link.bandwidth = bandwidth;
    }
    if let Some(mtu) = value.mtu {
        link.mtu = mtu;
    }
    if let Some(delay_ns) = value.delay_ns {
        link.delay_ns = delay_ns;
    }
    if let Some(jitter_ns) = value.jitter_ns {
        link.jitter_ns = jitter_ns;
    }

    account_write(link_account, &link, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Updated: {:?}", link);

    Ok(())
}
