//! Shared setup for [`super::subscribe_feed`] and [`super::unsubscribe_feed`].
//!
//! A feed is all-or-nothing: the user's subscribed groups are the union of the groups carried by the
//! feeds they hold. Both instructions name feeds and derive the groups that change, so a seat follows
//! the feed the caller named rather than being inferred from group membership. That is what makes two
//! feeds carrying the same group unambiguous.

use crate::{
    authorize::{authorize, split_trailing_permission},
    error::DoubleZeroError,
    pda::{get_accesspass_pda, get_globalstate_pda},
    processors::{feed::check_feed_metro_coverage, validation::validate_program_account},
    serializer::try_acc_write,
    state::{
        accesspass::{AccessPass, AccessPassType},
        device::Device,
        feed::Feed,
        globalstate::GlobalState,
        user::{User, UserType},
    },
};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};
use std::net::Ipv4Addr;

use super::subscribe::update_user_multicastgroup_roles;

/// The validated accounts and state both feed instructions work from.
pub struct FeedSubscriptionContext<'a, 'b> {
    pub accesspass_account: &'a AccountInfo<'b>,
    pub user_account: &'a AccountInfo<'b>,
    pub payer_account: &'a AccountInfo<'b>,
    pub permission_account: Option<&'a AccountInfo<'b>>,
    pub globalstate: GlobalState,
    pub accesspass: AccessPass,
    pub user: User,
    pub device: Device,
    /// The variable account section: feeds followed by multicast groups.
    pub variable: Vec<&'a AccountInfo<'b>>,
}

/// Parse and validate `[accesspass, user, globalstate, device, ..variable.., payer, system, perm?]`.
///
/// Applies every check both instructions share: the pass exists and is EdgeSeat, the user is
/// Multicast, and the device is the user's own.
pub fn load_context<'a, 'b>(
    program_id: &Pubkey,
    accounts: &'a [AccountInfo<'b>],
) -> Result<FeedSubscriptionContext<'a, 'b>, ProgramError> {
    let accounts_iter = &mut accounts.iter();

    let accesspass_account = next_account_info(accounts_iter)?;
    let user_account = next_account_info(accounts_iter)?;
    let gs_account = next_account_info(accounts_iter)?;
    let device_account = next_account_info(accounts_iter)?;

    let remaining: Vec<&AccountInfo> = accounts_iter.collect();
    let (payer_account, system_program, variable, permission_account) =
        split_trailing_permission(program_id, &remaining)?;

    assert!(payer_account.is_signer, "Payer must be a signer");
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );

    let (expected_globalstate_pda, _) = get_globalstate_pda(program_id);
    assert_eq!(
        gs_account.key, &expected_globalstate_pda,
        "Invalid GlobalState PDA"
    );
    let globalstate = GlobalState::try_from(gs_account)?;

    if accesspass_account.data_is_empty() {
        return Err(DoubleZeroError::AccessPassNotFound.into());
    }
    validate_program_account!(
        accesspass_account,
        program_id,
        writable = true,
        "AccessPass"
    );
    validate_program_account!(user_account, program_id, writable = true, "User");
    validate_program_account!(device_account, program_id, writable = false, "Device");

    let user = User::try_from(user_account)?;
    if user.user_type != UserType::Multicast {
        msg!(
            "a feed subscription requires a Multicast user, got {}",
            user.user_type
        );
        return Err(DoubleZeroError::EdgeSeatIsMulticastOnly.into());
    }
    if device_account.key != &user.device_pk {
        msg!(
            "device {} is not the user's device {}",
            device_account.key,
            user.device_pk
        );
        return Err(DoubleZeroError::UserDeviceMismatch.into());
    }
    let device = Device::try_from(device_account)?;

    let accesspass = AccessPass::try_from(accesspass_account)?;
    let (accesspass_pda, _) = get_accesspass_pda(program_id, &user.client_ip, &user.owner);
    let (accesspass_dynamic_pda, _) =
        get_accesspass_pda(program_id, &Ipv4Addr::UNSPECIFIED, &user.owner);
    assert!(
        accesspass_account.key == &accesspass_pda
            || accesspass_account.key == &accesspass_dynamic_pda,
        "Invalid AccessPass PDA",
    );
    if !matches!(accesspass.accesspass_type, AccessPassType::EdgeSeat(_)) {
        msg!(
            "AccessPass type {:?} carries no feeds; use UpdateMulticastGroupRoles",
            accesspass.accesspass_type
        );
        return Err(DoubleZeroError::EdgeSeatRequired.into());
    }

    Ok(FeedSubscriptionContext {
        accesspass_account,
        user_account,
        payer_account,
        permission_account,
        globalstate,
        accesspass,
        user,
        device,
        variable: variable.to_vec(),
    })
}

impl FeedSubscriptionContext<'_, '_> {
    /// The pass's own `user_payer`, a foundation member, or a holder of `required_flag`.
    pub fn authorize_payer(
        &self,
        program_id: &Pubkey,
        required_flag: u128,
        denied: DoubleZeroError,
    ) -> ProgramResult {
        if self.accesspass.user_payer == *self.payer_account.key
            || self
                .globalstate
                .foundation_allowlist
                .contains(self.payer_account.key)
        {
            return Ok(());
        }
        if authorize(
            program_id,
            &mut self.permission_account.into_iter(),
            self.payer_account.key,
            &self.globalstate,
            required_flag,
        )
        .is_err()
        {
            msg!(
                "AccessPass user_payer {:?} does not match payer {:?}",
                self.accesspass.user_payer,
                self.payer_account.key
            );
            return Err(denied.into());
        }
        Ok(())
    }
}

/// Read a run of Feed accounts, checking each is on the pass and serves the user's metro.
///
/// A feed listed in `held` is loaded even if no longer provisioned on the pass (its seat is gone,
/// so there is no metro to validate against); strict callers pass `&[]`.
pub fn load_feeds(
    program_id: &Pubkey,
    accesspass: &AccessPass,
    device_exchange: &Pubkey,
    feed_accounts: &[&AccountInfo],
    held: &[Pubkey],
) -> Result<Vec<(Pubkey, Feed)>, ProgramError> {
    let mut feeds: Vec<(Pubkey, Feed)> = Vec::with_capacity(feed_accounts.len());
    for feed_account in feed_accounts {
        let provisioned = accesspass
            .feed_seats()
            .iter()
            .any(|s| s.feed_key == *feed_account.key);
        if provisioned {
            check_feed_metro_coverage(
                program_id,
                accesspass,
                device_exchange,
                None,
                Some(feed_account),
            )?;
        } else if held.contains(feed_account.key) {
            if feed_account.owner != program_id {
                return Err(DoubleZeroError::InvalidAccountOwner.into());
            }
        } else {
            msg!(
                "Feed {} is not provisioned on the access pass",
                feed_account.key
            );
            return Err(DoubleZeroError::FeedNotOnAccessPass.into());
        }
        if feeds.iter().any(|(key, _)| key == feed_account.key) {
            msg!("feed {} passed more than once", feed_account.key);
            return Err(DoubleZeroError::InvalidArgument.into());
        }
        feeds.push((*feed_account.key, Feed::try_from(*feed_account)?));
    }
    Ok(feeds)
}

/// Reject unless `group_accounts` is exactly `expected`, so a stale client cannot half-apply a change.
pub fn check_group_accounts(group_accounts: &[&AccountInfo], expected: &[Pubkey]) -> ProgramResult {
    if group_accounts.len() != expected.len() {
        msg!(
            "passed {} MulticastGroup accounts but this call changes {}",
            group_accounts.len(),
            expected.len()
        );
        return Err(DoubleZeroError::InvalidArgument.into());
    }
    if let Some(missing) = expected
        .iter()
        .find(|group| !group_accounts.iter().any(|a| a.key == *group))
    {
        msg!("group {} changes here but was not passed", missing);
        return Err(DoubleZeroError::InvalidArgument.into());
    }
    Ok(())
}

/// Apply `subscriber` to every passed group and write each group account back.
///
/// The publisher role is carried through unchanged: clearing it would deallocate the user's `dz_ip`
/// as a side effect.
pub fn apply_groups(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    group_accounts: &[&AccountInfo],
    user: &mut User,
    payer_account: &AccountInfo,
    subscriber: bool,
) -> ProgramResult {
    for group_account in group_accounts {
        validate_program_account!(
            *group_account,
            program_id,
            writable = true,
            "MulticastGroup"
        );
        let carry_publisher = user.publishers.contains(group_account.key);
        let result =
            update_user_multicastgroup_roles(group_account, user, carry_publisher, subscriber)?;
        try_acc_write(&result.mgroup, group_account, payer_account, accounts)?;
    }
    Ok(())
}

/// Persist the pass and user after a seat change.
pub fn write_back(
    ctx_accesspass: &AccessPass,
    ctx_user: &User,
    accesspass_account: &AccountInfo,
    user_account: &AccountInfo,
    payer_account: &AccountInfo,
    accounts: &[AccountInfo],
) -> ProgramResult {
    try_acc_write(ctx_accesspass, accesspass_account, payer_account, accounts)?;
    try_acc_write(ctx_user, user_account, payer_account, accounts)?;
    Ok(())
}
