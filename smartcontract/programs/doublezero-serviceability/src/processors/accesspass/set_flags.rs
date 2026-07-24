use crate::{
    authorize::authorize,
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{
        accesspass::{AccessPass, ALLOW_MULTIPLE_IP, DZF_LOCKED},
        globalstate::GlobalState,
        permission::permission_flags,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};

/// Surgically set or clear individual bits of an existing access pass's `flags` byte. Each field is
/// tri-state: `None` leaves that flag untouched, `Some(true)` sets it, `Some(false)` clears it. Bits
/// not named here are always preserved, so this never clobbers unrelated flags. Adding a future flag
/// is just another `Option<bool>` field.
///
/// Gated on `ACCESS_PASS_ADMIN` (like the sibling SetAccessPass path).
#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq, Clone, Default)]
pub struct SetAccessPassFlagsArgs {
    pub allow_multiple_ip: Option<bool>,
    pub dzf_locked: Option<bool>,
}

pub fn process_set_access_pass_flags(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &SetAccessPassFlagsArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    // Account layout: [accesspass, globalstate, payer, system, (permission)] — matches the sibling
    // accesspass handlers, with authorize() consuming the trailing Permission account.
    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    #[cfg(test)]
    msg!("process_set_access_pass_flags({:?})", value);

    assert!(payer_account.is_signer, "Payer must be a signer");

    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    assert_eq!(
        accesspass_account.owner, program_id,
        "Invalid AccessPass Account Owner"
    );
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert!(
        accesspass_account.is_writable,
        "AccessPass Account is not writable"
    );

    // Reject a no-op call rather than charging for a write that changes nothing.
    if value.allow_multiple_ip.is_none() && value.dzf_locked.is_none() {
        msg!("SetAccessPassFlags requires at least one flag to set");
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    // All flag changes gate on ACCESS_PASS_ADMIN (Permission PDA or legacy fallback: foundation
    // allowlist, sentinel, or feed authority), matching the sibling SetAccessPass path.
    let globalstate = GlobalState::try_from(globalstate_account)?;
    authorize(
        program_id,
        accounts_iter,
        payer_account.key,
        &globalstate,
        permission_flags::ACCESS_PASS_ADMIN,
    )?;

    let mut accesspass = AccessPass::try_from(accesspass_account)?;

    // Mirror the sibling accesspass handlers: the feed authority may only touch passes it owns.
    // This is what stops the oracle (which holds ACCESS_PASS_ADMIN) from clearing dzf_locked on a
    // pass it did not create.
    if globalstate.feed_authority_pk == *payer_account.key && accesspass.owner != *payer_account.key
    {
        msg!("Feed authority can only modify access passes they own");
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if let Some(allow_multiple_ip) = value.allow_multiple_ip {
        if allow_multiple_ip {
            accesspass.flags |= ALLOW_MULTIPLE_IP;
        } else {
            accesspass.flags &= !ALLOW_MULTIPLE_IP;
        }
    }
    if let Some(dzf_locked) = value.dzf_locked {
        if dzf_locked {
            accesspass.flags |= DZF_LOCKED;
        } else {
            accesspass.flags &= !DZF_LOCKED;
        }
    }

    try_acc_write(&accesspass, accesspass_account, payer_account, accounts)?;

    msg!("Set access pass flags: [{}]", accesspass.flags_string());

    Ok(())
}
