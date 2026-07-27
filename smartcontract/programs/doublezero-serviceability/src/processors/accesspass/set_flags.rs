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
///
/// Deliberately does not derive `Default`: `default()` would be the all-`None` payload this
/// instruction rejects, and a `..Default::default()` construction site would silently swallow a
/// future flag field instead of failing to compile.
#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq, Clone)]
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
    //
    // Unlike set.rs / set_feeds.rs there is intentionally no PDA re-derivation of the accesspass
    // account: the args deliberately carry no client_ip / user_payer seeds. The account is still
    // fully validated below (program-owned + AccessPass discriminator via try_from), and an
    // ACCESS_PASS_ADMIN caller may already target any pass, so threading the seeds back into the
    // args would only add redundant instruction arguments. Don't "fix" this by re-adding them.
    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
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

    // Reject a no-op call rather than charging for a write that changes nothing. Runs after
    // authorize() so an unauthenticated caller gets NotAllowed rather than a payload diagnostic.
    //
    // Destructured rather than compared against a Default so a future flag field cannot be
    // silently omitted here: adding one is a hard error at this pattern (E0027), and forgetting to
    // fold the new binding into the condition below is an unused-variable warning, which CI fails
    // on under `-Dwarnings`.
    let SetAccessPassFlagsArgs {
        allow_multiple_ip,
        dzf_locked,
    } = *value;
    if allow_multiple_ip.is_none() && dzf_locked.is_none() {
        msg!("SetAccessPassFlags requires at least one flag to update");
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    let mut accesspass = AccessPass::try_from(accesspass_account)?;

    // Mirror the sibling accesspass handlers: the feed authority may only touch passes it owns.
    // This is what stops the oracle (which holds ACCESS_PASS_ADMIN) from clearing dzf_locked on a
    // pass it did not create.
    //
    // DEPENDENCY: this holds only while the oracle's key lives in the legacy
    // `globalstate.feed_authority_pk`. Once the oracle authorizes purely via a Permission account
    // granting ACCESS_PASS_ADMIN and this legacy field is cleared or reassigned, the condition is
    // never true and the guard silently no-ops — losing exactly the property dzf_locked exists to
    // guarantee. The durable fix is to fence *any* non-foundation/sentinel ACCESS_PASS_ADMIN caller
    // to passes it owns, which belongs with the feed-authority -> Permission migration because the
    // same guard is duplicated in accesspass/set.rs and accesspass/close.rs. Revisit all three
    // together at that point.
    if globalstate.feed_authority_pk == *payer_account.key && accesspass.owner != *payer_account.key
    {
        msg!("Feed authority can only modify access passes they own");
        return Err(DoubleZeroError::NotAllowed.into());
    }

    if let Some(allow_multiple_ip) = allow_multiple_ip {
        if allow_multiple_ip {
            accesspass.flags |= ALLOW_MULTIPLE_IP;
        } else {
            accesspass.flags &= !ALLOW_MULTIPLE_IP;
        }
    }
    if let Some(dzf_locked) = dzf_locked {
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

#[cfg(test)]
mod tests {
    use super::*;

    /// Each `Option<bool>` is a 1-byte tag plus 1 byte of payload when `Some`, so the tri-state
    /// encoding is 1 byte per `None` field and 2 per `Some` field.
    #[test]
    fn test_set_access_pass_flags_args_wire_format() {
        let cases: [(SetAccessPassFlagsArgs, &[u8]); 4] = [
            (
                SetAccessPassFlagsArgs {
                    allow_multiple_ip: None,
                    dzf_locked: None,
                },
                &[0, 0],
            ),
            (
                SetAccessPassFlagsArgs {
                    allow_multiple_ip: None,
                    dzf_locked: Some(true),
                },
                &[0, 1, 1],
            ),
            (
                SetAccessPassFlagsArgs {
                    allow_multiple_ip: Some(true),
                    dzf_locked: Some(false),
                },
                &[1, 1, 1, 0],
            ),
            (
                SetAccessPassFlagsArgs {
                    allow_multiple_ip: Some(false),
                    dzf_locked: Some(true),
                },
                &[1, 0, 1, 1],
            ),
        ];

        for (args, expected) in cases {
            let payload = borsh::to_vec(&args).unwrap();
            assert_eq!(payload, expected, "encoding of {args:?}");
            assert_eq!(
                SetAccessPassFlagsArgs::try_from(&payload[..]).unwrap(),
                args,
                "round-trip of {args:?}"
            );
        }
    }

    /// `BorshDeserializeIncremental` must accept a payload from a sender that predates a later
    /// field and default the missing tail to `None`. A truncated payload therefore can never
    /// accidentally set a flag: at worst it decodes to the all-`None` payload the processor
    /// rejects with `InvalidArgument`.
    #[test]
    fn test_set_access_pass_flags_args_incremental_decode_truncated_payload() {
        // Only the first field on the wire: allow_multiple_ip = Some(false).
        let old_payload = [1u8, 0];
        let args = SetAccessPassFlagsArgs::try_from(&old_payload[..]).unwrap();
        assert_eq!(args.allow_multiple_ip, Some(false));
        assert_eq!(args.dzf_locked, None, "missing tail defaults to None");

        // An empty payload defaults every field to None.
        let args = SetAccessPassFlagsArgs::try_from(&[][..]).unwrap();
        assert_eq!(args.allow_multiple_ip, None);
        assert_eq!(args.dzf_locked, None);
    }
}
