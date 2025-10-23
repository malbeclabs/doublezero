use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    helper::*,
    state::{contributor::Contributor, link::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::validate_account_code;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct LinkUpdateArgs {
    pub code: Option<String>,
    pub contributor_pk: Option<Pubkey>,
    pub tunnel_type: Option<LinkLinkType>,
    pub bandwidth: Option<u64>,
    pub mtu: Option<u32>,
    pub delay_ns: Option<u64>,
    pub jitter_ns: Option<u64>,
    pub status: Option<LinkStatus>,
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
    let contributor_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_link({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

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
    let contributor = Contributor::try_from(contributor_account)?;

    if contributor.owner != *payer_account.key
        && !globalstate.foundation_allowlist.contains(payer_account.key)
    {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut link: Link = Link::try_from(link_account)?;

    if let Some(ref code) = value.code {
        link.code = validate_account_code(code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;
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
    if let Some(status) = value.status {
        // Only allow to update the status if the payer is in the foundation allowlist
        if !globalstate.foundation_allowlist.contains(payer_account.key) {
            return Err(DoubleZeroError::NotAllowed.into());
        }
        link.status = status;
    }

    account_write(link_account, &link, payer_account, system_program)?;

    #[cfg(test)]
    msg!("Updated: {:?}", link);

    Ok(())
}
