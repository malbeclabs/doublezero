use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{
        accesspass::{AccessPass, AccessPassStatus},
        globalstate::GlobalState,
        user::User,
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

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct TransferUserOwnershipArgs {}

impl fmt::Debug for TransferUserOwnershipArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "")
    }
}

/// Transfers ownership of a user account from one owner to another.
///
/// Account layout: [user, globalstate, old_access_pass, new_access_pass, payer, system]
///
/// Validation:
/// - old_access_pass.user_payer == globalstate.feed_authority_pk
/// - old_access_pass.client_ip == user.client_ip
/// - new_access_pass.client_ip == user.client_ip
/// - user.owner is updated to new_access_pass.user_payer
pub fn process_transfer_user_ownership(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &TransferUserOwnershipArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let old_access_pass_account = next_account_info(accounts_iter)?;
    let new_access_pass_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_transfer_user_ownership({:?})", _value);

    // Check if the payer is a signer
    assert!(payer_account.is_signer, "Payer must be a signer");

    // Check the owner of the accounts
    assert_eq!(user_account.owner, program_id, "Invalid User Account Owner");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        old_access_pass_account.owner, program_id,
        "Invalid Old AccessPass Account Owner"
    );
    assert_eq!(
        new_access_pass_account.owner, program_id,
        "Invalid New AccessPass Account Owner"
    );

    // Check if mutable accounts are writable
    assert!(user_account.is_writable, "User Account is not writable");
    assert!(
        old_access_pass_account.is_writable,
        "Old AccessPass Account is not writable"
    );
    assert!(
        new_access_pass_account.is_writable,
        "New AccessPass Account is not writable"
    );

    // Deserialize accounts
    let globalstate = GlobalState::try_from(globalstate_account)?;

    let is_foundation_member = globalstate.foundation_allowlist.contains(payer_account.key);

    let mut user = User::try_from(user_account)?;
    let mut old_access_pass = AccessPass::try_from(old_access_pass_account)?;
    let mut new_access_pass = AccessPass::try_from(new_access_pass_account)?;

    // Authorization: old access pass must belong to the feed authority, OR the payer
    // must be a foundation allowlist member (foundation members can transfer any user)
    if old_access_pass.user_payer != globalstate.feed_authority_pk && !is_foundation_member {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Validate old access pass: client_ip must match user's client_ip
    if old_access_pass.client_ip != user.client_ip {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Validate new access pass: client_ip must match user's client_ip
    if new_access_pass.client_ip != user.client_ip {
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Update old access pass: decrement connection count and update status
    old_access_pass.connection_count = old_access_pass.connection_count.saturating_sub(1);
    old_access_pass.status = if old_access_pass.connection_count > 0 {
        AccessPassStatus::Connected
    } else {
        AccessPassStatus::Disconnected
    };

    // Update new access pass: increment connection count and set connected
    new_access_pass.connection_count += 1;
    new_access_pass.status = AccessPassStatus::Connected;

    // Merge multicast allowlists from old access pass to new access pass
    for pk in &old_access_pass.mgroup_pub_allowlist {
        if !new_access_pass.mgroup_pub_allowlist.contains(pk) {
            new_access_pass.mgroup_pub_allowlist.push(*pk);
        }
    }
    for pk in &old_access_pass.mgroup_sub_allowlist {
        if !new_access_pass.mgroup_sub_allowlist.contains(pk) {
            new_access_pass.mgroup_sub_allowlist.push(*pk);
        }
    }

    // Transfer ownership
    user.owner = new_access_pass.user_payer;

    try_acc_write(&user, user_account, payer_account, accounts)?;
    try_acc_write(
        &old_access_pass,
        old_access_pass_account,
        payer_account,
        accounts,
    )?;
    try_acc_write(
        &new_access_pass,
        new_access_pass_account,
        payer_account,
        accounts,
    )?;

    #[cfg(test)]
    msg!("Transferred ownership: {:?}", user);

    Ok(())
}
