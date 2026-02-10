use crate::{
    error::DoubleZeroError,
    pda::get_mgroup_allowlist_entry_pda,
    serializer::try_acc_close,
    state::{
        globalstate::GlobalState, mgroup_allowlist_entry::MGroupAllowlistType,
        multicastgroup::MulticastGroup,
    },
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
use std::net::Ipv4Addr;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct RemoveMulticastGroupSubAllowlistArgs {
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
}

impl fmt::Debug for RemoveMulticastGroupSubAllowlistArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "client_ip: {}, user_payer: {}",
            self.client_ip, self.user_payer
        )
    }
}

pub fn process_remove_multicast_sub_allowlist(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &RemoveMulticastGroupSubAllowlistArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let mgroup_account = next_account_info(accounts_iter)?;
    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let mgroup_al_entry_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_remove_multicast_sub_allowlist({:?})", _value);

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
    let globalstate = GlobalState::try_from(globalstate_account)?;

    // Check whether mgroup is authorized
    let is_authorized = (mgroup.owner == *payer_account.key)
        || globalstate.foundation_allowlist.contains(payer_account.key);
    if !is_authorized {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Close the MGroupAllowlistEntry PDA
    if !mgroup_al_entry_account.data_is_empty() {
        let (expected_pda, _) = get_mgroup_allowlist_entry_pda(
            program_id,
            accesspass_account.key,
            mgroup_account.key,
            MGroupAllowlistType::Subscriber as u8,
        );
        assert_eq!(
            mgroup_al_entry_account.key, &expected_pda,
            "Invalid MGroupAllowlistEntry PDA"
        );
        assert_eq!(
            mgroup_al_entry_account.owner, program_id,
            "Invalid MGroupAllowlistEntry Account Owner"
        );

        try_acc_close(mgroup_al_entry_account, payer_account)?;
    }

    #[cfg(test)]
    msg!("Updated: {:?}", mgroup);

    Ok(())
}
