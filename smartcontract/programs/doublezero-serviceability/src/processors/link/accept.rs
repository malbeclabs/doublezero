use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{contributor::Contributor, device::Device, link::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct LinkAcceptArgs {
    pub side_z_iface_name: String,
}

impl fmt::Debug for LinkAcceptArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "side_z_iface_name: {}", self.side_z_iface_name,)
    }
}

pub fn process_accept_link(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &LinkAcceptArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    /*  Accounts prefixed with an underscore are not currently used.
        They are kept for backward compatibility and may be removed in future releases.
    */
    let link_account = next_account_info(accounts_iter)?;
    let contributor_account = next_account_info(accounts_iter)?;
    let side_z_account = next_account_info(accounts_iter)?;
    let _globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_accept_link({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(link_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        contributor_account.owner, program_id,
        "Invalid Contributor Account Owner"
    );
    assert_eq!(
        side_z_account.owner, program_id,
        "Invalid Side Z Account Owner"
    );
    // Check if the account is writable
    assert!(link_account.is_writable, "PDA Account is not writable");

    // Validate Contributor Owner
    let contributor = Contributor::try_from(contributor_account)?;
    if contributor.owner != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Validate Link Status
    let mut link: Link = Link::try_from(link_account)?;
    if link.status != LinkStatus::Requested {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    // Validate Side Z Device
    let side_z_dev = Device::try_from(side_z_account)?;
    if side_z_dev.contributor_pk != *contributor_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if !side_z_dev
        .interfaces
        .iter()
        .any(|iface| iface.into_current_version().name == value.side_z_iface_name)
    {
        #[cfg(test)]
        msg!("{:?}", side_z_dev);

        return Err(DoubleZeroError::InvalidInterfaceName.into());
    }
    link.side_z_iface_name = value.side_z_iface_name.clone();
    link.status = LinkStatus::Pending;

    try_acc_write(&link, link_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Accepted: {:?}", link);

    Ok(())
}
