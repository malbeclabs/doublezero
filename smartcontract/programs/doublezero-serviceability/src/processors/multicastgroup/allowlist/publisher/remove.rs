use crate::{
    error::DoubleZeroError,
    globalstate::globalstate_get,
    state::{accesspass::AccessPass, accounttype::AccountTypeInfo, multicastgroup::MulticastGroup},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::resize_account::resize_account_if_needed;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::net::Ipv4Addr;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct RemoveMulticastGroupPubAllowlistArgs {
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
}

impl fmt::Debug for RemoveMulticastGroupPubAllowlistArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "client_ip: {}, user_payer: {}",
            self.client_ip, self.user_payer
        )
    }
}

pub fn process_remove_multicast_pub_allowlist(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &RemoveMulticastGroupPubAllowlistArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let mgroup_account = next_account_info(accounts_iter)?;
    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_remove_multicast_pub_allowlist({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(
        mgroup_account.owner, program_id,
        "Invalid PDA Account Owner"
    );
    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    assert_eq!(
        accesspass_account.owner, program_id,
        "Invalid Accesspass Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_program::system_program::id(),
        "Invalid System Program Account Owner"
    );
    // Check if the account is writable
    assert!(mgroup_account.is_writable, "PDA Account is not writable");

    // Parse the global state account
    let mgroup = MulticastGroup::try_from(mgroup_account)?;
    let globalstate = globalstate_get(globalstate_account)?;

    // Check whether mgroup is authorized
    let is_authorized = (mgroup.owner == *payer_account.key)
        || globalstate.foundation_allowlist.contains(payer_account.key);
    if !is_authorized {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut accesspass = AccessPass::try_from(accesspass_account)?;
    assert!(
        accesspass.client_ip == value.client_ip,
        "AccessPass client_ip does not match"
    );
    assert!(
        accesspass.user_payer == value.user_payer,
        "AccessPass user_payer does not match"
    );

    accesspass
        .mgroup_pub_allowlist
        .retain(|x| x != mgroup_account.key);

    resize_account_if_needed(
        accesspass_account,
        payer_account,
        accounts,
        accesspass.size(),
    )?;
    accesspass.try_serialize(accesspass_account)?;

    #[cfg(test)]
    msg!("Updated: {:?}", mgroup);

    Ok(())
}
