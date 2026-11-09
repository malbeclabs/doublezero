use crate::{
    authorize::{authorize, feed_authority_role},
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{
        accesspass::{AccessPass, AccessPassStatus},
        globalstate::GlobalState,
        permission::permission_flags,
    },
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};
use std::net::Ipv4Addr;

/// The user counts an admin sets on an existing access pass after it counts the users on the pass
/// offchain. `connection_count` is not a new value: the program sets it to the sum of the
/// unicast users, the public multicast users and the multicast users that joined only private
/// groups. The pass does not store the private count.
///
/// The `expected_*` fields are the counts the admin read when it counted the users. The program
/// writes only when all three still match the pass, so a user created or deleted between the
/// count and the transaction fails the write instead of being lost from the counts.
///
/// Decoded strictly, unlike most instruction arguments. A short payload is refused, because an
/// incremental decode would read the missing counts as 0 and silently zero the pass.
#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub struct SetAccessPassCountsArgs {
    pub expected_connection_count: u16,
    pub expected_unicast_user_count: u16,
    pub expected_public_multicast_user_count: u16,
    pub unicast_user_count: u16,
    pub public_multicast_user_count: u16,
    pub private_multicast_user_count: u16,
}

impl TryFrom<&[u8]> for SetAccessPassCountsArgs {
    type Error = DoubleZeroError;

    fn try_from(mut value: &[u8]) -> Result<Self, DoubleZeroError> {
        SetAccessPassCountsArgs::deserialize(&mut value)
            .map_err(|_| DoubleZeroError::InvalidArgument)
    }
}

/// Replace the user counts of an existing access pass, if they still hold the expected values. This
/// corrects counts that do not match the
/// users on the pass: counts from before every pass type kept them, and counts left behind when a
/// delete was charged to a different pass. Such a count blocks `Close*AccessPass`.
///
/// Gated on `ACCESS_PASS_ADMIN`, like the sibling access pass handlers, and the feed authority may
/// only touch passes it owns.
pub fn process_set_access_pass_counts(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &SetAccessPassCountsArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    // Account layout: [accesspass, globalstate, payer, system, (permission)], the same as
    // SetAccessPassFlags. The pass is named by its address, so there are no PDA seeds to check:
    // try_from below checks the AccessPass discriminator, and an ACCESS_PASS_ADMIN may target any
    // pass.
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
    msg!("process_set_access_pass_counts({:?})", value);

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

    let globalstate = GlobalState::load(program_id, globalstate_account)?;
    // Read before authorize() consumes it from the iterator.
    let feed_role = feed_authority_role(
        program_id,
        accounts_iter.clone().next(),
        payer_account.key,
        &globalstate,
    );
    authorize(
        program_id,
        accounts_iter,
        payer_account.key,
        &globalstate,
        permission_flags::ACCESS_PASS_ADMIN,
    )?;

    let Some(connection_count) = value
        .unicast_user_count
        .checked_add(value.public_multicast_user_count)
        .and_then(|count| count.checked_add(value.private_multicast_user_count))
    else {
        msg!(
            "unicast users {} plus public multicast users {} plus private multicast users {} must fit in a u16",
            value.unicast_user_count,
            value.public_multicast_user_count,
            value.private_multicast_user_count
        );
        return Err(DoubleZeroError::InvalidArgument.into());
    };

    let mut accesspass = AccessPass::try_from(accesspass_account)?;

    // The feed authority holds ACCESS_PASS_ADMIN, so hold it to the passes it owns, as the
    // sibling handlers do.
    if feed_role.limited_to_own_passes() && accesspass.owner != *payer_account.key {
        msg!("Feed authority can only modify access passes they own");
        return Err(DoubleZeroError::NotAllowed.into());
    }

    // Compare-and-swap: refuse to write over counts that changed since the admin read them.
    if accesspass.connection_count != value.expected_connection_count
        || accesspass.unicast_user_count != value.expected_unicast_user_count
        || accesspass.public_multicast_user_count != value.expected_public_multicast_user_count
    {
        msg!(
            "Expected connections {}, unicast users {}, multicast users {}, but the pass has connections {}, unicast users {}, multicast users {}",
            value.expected_connection_count,
            value.expected_unicast_user_count,
            value.expected_public_multicast_user_count,
            accesspass.connection_count,
            accesspass.unicast_user_count,
            accesspass.public_multicast_user_count
        );
        return Err(DoubleZeroError::AccessPassCountsChanged.into());
    }

    msg!(
        "Set access pass counts: connections {} -> {}, unicast users {} -> {}, multicast users {} -> {}",
        accesspass.connection_count,
        connection_count,
        accesspass.unicast_user_count,
        value.unicast_user_count,
        accesspass.public_multicast_user_count,
        value.public_multicast_user_count
    );

    accesspass.connection_count = connection_count;
    accesspass.unicast_user_count = value.unicast_user_count;
    accesspass.public_multicast_user_count = value.public_multicast_user_count;

    // Leave the pass in the state the last user delete would have left it in.
    if connection_count > 0 {
        accesspass.status = AccessPassStatus::Connected;
    } else {
        if accesspass.status == AccessPassStatus::Connected {
            accesspass.status = AccessPassStatus::Disconnected;
        }
        if accesspass.allow_multiple_ip() {
            accesspass.client_ip = Ipv4Addr::UNSPECIFIED;
        }
    }

    try_acc_write(&accesspass, accesspass_account, payer_account, accounts)?;

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_set_access_pass_counts_args_wire_format() {
        let args = SetAccessPassCountsArgs {
            expected_connection_count: 4,
            expected_unicast_user_count: 5,
            expected_public_multicast_user_count: 0x0206,
            unicast_user_count: 2,
            public_multicast_user_count: 0x0103,
            private_multicast_user_count: 7,
        };
        let payload = borsh::to_vec(&args).unwrap();
        assert_eq!(payload, [4, 0, 5, 0, 6, 2, 2, 0, 3, 1, 7, 0]);
        assert_eq!(
            SetAccessPassCountsArgs::try_from(&payload[..]).unwrap(),
            args
        );
    }

    /// A payload that stops before the last count is refused rather than read as a zero count.
    #[test]
    fn test_set_access_pass_counts_args_refuses_a_short_payload() {
        assert_eq!(
            SetAccessPassCountsArgs::try_from(&[4u8, 0, 5, 0, 6, 2, 2, 0, 3, 1, 7][..])
                .unwrap_err(),
            DoubleZeroError::InvalidArgument
        );
        assert_eq!(
            SetAccessPassCountsArgs::try_from(&[][..]).unwrap_err(),
            DoubleZeroError::InvalidArgument
        );
    }
}
