use crate::{
    error::DoubleZeroError,
    format_option,
    helper::format_option_displayable,
    serializer::try_acc_write,
    state::{globalstate::GlobalState, user::*},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use doublezero_program_common::types::NetworkV4;
#[cfg(test)]
use solana_program::msg;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};
use std::fmt;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct UserUpdateArgs {
    pub user_type: Option<UserType>,
    pub cyoa_type: Option<UserCYOA>,
    pub dz_ip: Option<std::net::Ipv4Addr>,
    pub tunnel_id: Option<u16>,
    pub tunnel_net: Option<NetworkV4>,
    pub validator_pubkey: Option<Pubkey>,
    pub clear_publishers: Option<bool>,
    pub clear_subscribers: Option<bool>,
}

impl fmt::Debug for UserUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "user_type: {}, cyoa_type: {}, dz_ip: {}, tunnel_id: {}, tunnel_net: {}, validator_pubkey: {}",
            format_option!(self.user_type),
            format_option!(self.cyoa_type),
            format_option!(self.dz_ip),
            format_option!(self.tunnel_id),
            format_option!(self.tunnel_net),
            format_option!(self.validator_pubkey),
        )
    }
}

pub fn process_update_user(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &UserUpdateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_update_user({:?})", value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(user_account.owner, program_id, "Invalid PDA Account Owner");
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
    assert!(user_account.is_writable, "PDA Account is not writable");

    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !globalstate.foundation_allowlist.contains(payer_account.key) {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    let mut user: User = User::try_from(user_account)?;

    if let Some(value) = value.dz_ip {
        user.dz_ip = value;
    }
    if let Some(value) = value.tunnel_id {
        user.tunnel_id = value;
    }
    if let Some(value) = value.tunnel_net {
        user.tunnel_net = value;
    }
    if let Some(value) = value.user_type {
        user.user_type = value;
    }
    if let Some(value) = value.cyoa_type {
        user.cyoa_type = value;
    }
    if let Some(value) = value.validator_pubkey {
        user.validator_pubkey = value;
    }
    if value.clear_publishers == Some(true) {
        user.publishers.clear();
    }
    if value.clear_subscribers == Some(true) {
        user.subscribers.clear();
    }

    try_acc_write(&user, user_account, payer_account, accounts)?;

    #[cfg(test)]
    msg!("Updated: {:?}", user);

    Ok(())
}
