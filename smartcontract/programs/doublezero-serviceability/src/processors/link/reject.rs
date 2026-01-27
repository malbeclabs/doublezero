use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{contributor::Contributor, device::Device, globalstate::GlobalState, link::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::types::NetworkV4;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};
use std::fmt;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct LinkRejectArgs {
    pub reason: String,
}

impl fmt::Debug for LinkRejectArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "reason: {}", self.reason)
    }
}

pub fn process_reject_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkRejectArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let link_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // Check the owner of link and globalstate accounts first
    assert_eq!(link_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert!(link_account.is_writable, "PDA Account is not writable");

    let globalstate = GlobalState::try_from(globalstate_account)?;
    let mut link: Link = Link::try_from(link_account)?;

    // Allow foundation to reject Pending links
    // Allow Contributor B to reject Requested links
    let payer_account = match link.status {
        LinkStatus::Pending => {
            let payer_account = next_account_info(accounts_iter)?;
            let system_program = next_account_info(accounts_iter)?;

            assert!(payer_account.is_signer, "Payer must be a signer");
            assert_eq!(
                *system_program.unsigned_key(),
                solana_program::system_program::id(),
                "Invalid System Program Account Owner"
            );

            if !globalstate.foundation_allowlist.contains(payer_account.key) {
                return Err(DoubleZeroError::NotAllowed.into());
            }
            payer_account
        }
        LinkStatus::Requested => {
            let contributor_account = next_account_info(accounts_iter)?;
            let side_z_account = next_account_info(accounts_iter)?;
            let payer_account = next_account_info(accounts_iter)?;
            let system_program = next_account_info(accounts_iter)?;

            assert_eq!(
                contributor_account.owner, program_id,
                "Invalid PDA Account Owner"
            );
            assert_eq!(
                side_z_account.owner, program_id,
                "Invalid Side Z Account Owner"
            );
            assert!(payer_account.is_signer, "Payer must be a signer");
            assert_eq!(
                *system_program.unsigned_key(),
                solana_program::system_program::id(),
                "Invalid System Program Account Owner"
            );

            let contributor = Contributor::try_from(contributor_account)?;
            let side_z_dev = Device::try_from(side_z_account)?;

            if contributor.owner != *payer_account.key
                || side_z_dev.contributor_pk != *contributor_account.key
                || *side_z_account.key != link.side_z_pk
            {
                return Err(DoubleZeroError::NotAllowed.into());
            }
            payer_account
        }
        _ => return Err(DoubleZeroError::InvalidStatus.into()),
    };

    #[cfg(test)]
    msg!("process_activate_link({:?})", value);

    link.tunnel_id = 0;
    link.tunnel_net = NetworkV4::default();
    link.status = LinkStatus::Rejected;
    msg!("Reason: {:?}", value.reason);

    try_acc_write(&link, link_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Rejected: {:?}", link);

    Ok(())
}
