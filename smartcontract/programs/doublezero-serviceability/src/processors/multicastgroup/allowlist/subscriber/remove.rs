use crate::{
    error::DoubleZeroError,
    pda::get_accesspass_pda,
    processors::validation::validate_program_account,
    serializer::try_acc_write,
    state::{accesspass::AccessPass, globalstate::GlobalState, multicastgroup::MulticastGroup},
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
    value: &RemoveMulticastGroupSubAllowlistArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let mgroup_account = next_account_info(accounts_iter)?;
    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_remove_multicast_sub_allowlist({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    validate_program_account!(
        mgroup_account,
        program_id,
        writable = true,
        "MulticastGroup"
    );
    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    validate_program_account!(
        accesspass_account,
        program_id,
        writable = false,
        "AccessPass"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    // Parse the global state account
    let mgroup = MulticastGroup::try_from(mgroup_account)?;
    let globalstate = GlobalState::try_from(globalstate_account)?;

    // Check whether mgroup is authorized
    let is_authorized = (mgroup.owner == *payer_account.key)
        || globalstate.sentinel_authority_pk == *payer_account.key
        || globalstate.feed_authority_pk == *payer_account.key
        || globalstate.foundation_allowlist.contains(payer_account.key);
    if !is_authorized {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut accesspass = AccessPass::try_from(accesspass_account)?;

    // Validate PDA using the stored user_payer. For allow_multiple_ip passes, also accept the dynamic (0.0.0.0) PDA.
    let (expected_pda, _) =
        get_accesspass_pda(program_id, &value.client_ip, &accesspass.user_payer);
    let (dynamic_pda, _) =
        get_accesspass_pda(program_id, &Ipv4Addr::UNSPECIFIED, &accesspass.user_payer);
    assert!(
        accesspass_account.key == &expected_pda
            || (accesspass.allow_multiple_ip() && accesspass_account.key == &dynamic_pda),
        "Invalid AccessPass PDA"
    );

    // For allow_multiple_ip passes, the stored client_ip is 0.0.0.0 regardless of the connecting IP
    if !accesspass.allow_multiple_ip() {
        assert!(
            accesspass.client_ip == value.client_ip,
            "AccessPass client_ip does not match"
        );
    }
    // Feed authority may operate on access passes with a different user_payer
    if globalstate.feed_authority_pk != *payer_account.key {
        assert!(
            accesspass.user_payer == value.user_payer,
            "AccessPass user_payer does not match"
        );
    }

    accesspass
        .mgroup_sub_allowlist
        .retain(|x| x != mgroup_account.key);

    try_acc_write(&accesspass, accesspass_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Updated: {:?}", mgroup);

    Ok(())
}
