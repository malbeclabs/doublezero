use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{globalstate::GlobalState, multicastgroup::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct MulticastGroupActivateArgs {
    #[incremental(default = std::net::Ipv4Addr::UNSPECIFIED)]
    pub multicast_ip: std::net::Ipv4Addr,
}

impl fmt::Debug for MulticastGroupActivateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "",)
    }
}

pub fn process_activate_multicastgroup(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &MulticastGroupActivateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let multicastgroup_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_activate_multicastgroup({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        multicastgroup_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    // Check if the account is writable
    assert!(
        multicastgroup_account.is_writable,
        "PDA Account is not writable"
    );

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if globalstate.activator_authority_pk != *payer_account.key {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut multicastgroup: MulticastGroup = MulticastGroup::try_from(multicastgroup_account)?;

    if multicastgroup.status != MulticastGroupStatus::Pending {
        return Err(DoubleZeroError::InvalidStatus.into());
    }

    multicastgroup.multicast_ip = value.multicast_ip;
    multicastgroup.status = MulticastGroupStatus::Activated;

    try_acc_write(
        &multicastgroup,
        multicastgroup_account,
        payer_account,
        accounts,
    )?;

    msg!("Activated: {:?}", multicastgroup);

    Ok(())
}
