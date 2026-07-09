use crate::{
    authorize::authorize,
    error::DoubleZeroError,
    pda::get_accesspass_pda,
    serializer::try_acc_write,
    state::{
        accesspass::{AccessPass, AccessPassType, FeedSeat},
        feed::Feed,
        globalstate::GlobalState,
        permission::permission_flags,
    },
};
use borsh::{BorshDeserialize, BorshSerialize};
use borsh_incremental::BorshDeserializeIncremental;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    clock::Clock,
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
    sysvar::Sysvar,
};
use std::net::Ipv4Addr;

/// Upper bound on the number of feed seats provisioned in a single call. Bounds the AccessPass
/// account growth and the duplicate scan below; mirrors the catalog-side feed limits.
pub const MAX_ACCESS_PASS_FEEDS: usize = 64;

/// Per-feed provisioning input paired by position with the passed `Feed` accounts. The `feed_key`
/// is read from the account (not duplicated here) and `current_users` is maintained server-side,
/// so the caller supplies each feed's billing state: the current cap, the future cap, the
/// anniversary day, and the window/termination boundaries (see [`FeedSeat`]). The granter computes
/// `window_end` / `terminates_at` with anniversary clamping and passes them.
#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub struct FeedSeatConfig {
    pub max_users: u8,
    pub max_future_users: u8,
    pub anniversary_day: u8,
    pub window_end: i64,
    pub terminates_at: i64,
}

/// Provision the feed_keys (SKU seats) onto an EdgeSeat access pass. The provisioning actor is the
/// oracle, authorized via its `ACCESS_PASS_ADMIN` Permission — not the deprecated `feed_authority`
/// slot. `current_users` is preserved for feeds already present on the pass; caps come from the
/// caller (seeded from the coupon).
///
/// Each entry in `feeds` is paired by position with a `Feed` account (see
/// `process_set_access_pass_feeds` for the account layout).
#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq, Clone)]
pub struct SetAccessPassFeedsArgs {
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
    pub feeds: Vec<FeedSeatConfig>,
}

pub fn process_set_access_pass_feeds(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &SetAccessPassFeedsArgs,
) -> ProgramResult {
    if value.feeds.len() > MAX_ACCESS_PASS_FEEDS {
        msg!(
            "SetAccessPassFeeds accepts at most {} feeds, got {}",
            MAX_ACCESS_PASS_FEEDS,
            value.feeds.len()
        );
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    let accounts_iter = &mut accounts.iter();

    // Account layout: [accesspass, globalstate, feed_0 .. feed_{N-1}, payer, system, (permission)].
    // The trailing [payer, system, permission] convention matches the sibling accesspass handlers.
    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;

    // One Feed account per requested seat, in the same order as `value.feeds`.
    let mut feed_accounts = Vec::with_capacity(value.feeds.len());
    for _ in 0..value.feeds.len() {
        feed_accounts.push(next_account_info(accounts_iter)?);
    }

    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    assert!(payer_account.is_signer, "Payer must be a signer");
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

    let (expected_pda, _) = get_accesspass_pda(program_id, &value.client_ip, &value.user_payer);
    assert_eq!(
        accesspass_account.key, &expected_pda,
        "Invalid AccessPass PubKey"
    );

    // Provisioning actor authorized via ACCESS_PASS_ADMIN (Permission PDA or legacy fallback).
    let globalstate = GlobalState::try_from(globalstate_account)?;
    authorize(
        program_id,
        accounts_iter,
        payer_account.key,
        &globalstate,
        permission_flags::ACCESS_PASS_ADMIN,
    )?;

    let mut accesspass = AccessPass::try_from(accesspass_account)?;

    // Only EdgeSeat passes carry feed seats. Require the pass to already be EdgeSeat (the oracle
    // sets the type via SetAccessPass first) so this instruction can't silently convert a pass of
    // another type (e.g. SolanaValidator) into an EdgeSeat.
    if !matches!(accesspass.accesspass_type, AccessPassType::EdgeSeat(_)) {
        msg!("SetAccessPassFeeds requires an EdgeSeat access pass");
        return Err(DoubleZeroError::InvalidArgument.into());
    }
    let prior_seats = accesspass.feed_seats().to_vec();

    // Current on-chain time, used to reject billing windows that have already elapsed. Read via the
    // Clock syscall (no account to pass); the granter is trusted, so this is a sanity floor rather
    // than the authoritative timing (the oracle drives the actual flip/removal).
    let now = Clock::get()?.unix_timestamp;

    // Validate each referenced Feed and preserve live counts. Feeds are not reference-counted, so
    // this only reads the Feed accounts (to bind feed_key and confirm the feed exists) and never
    // writes them; dropping a feed from a pass needs nothing here.
    let mut new_seats: Vec<FeedSeat> = Vec::with_capacity(value.feeds.len());
    for (config, feed_account) in value.feeds.iter().zip(feed_accounts.iter()) {
        assert_eq!(feed_account.owner, program_id, "Invalid Feed Account Owner");
        let feed_key = *feed_account.key;

        // Reject a feed_key listed more than once: it would write duplicate seats.
        if new_seats.iter().any(|s| s.feed_key == feed_key) {
            msg!("Duplicate feed_key in SetAccessPassFeeds: {}", feed_key);
            return Err(DoubleZeroError::InvalidArgument.into());
        }

        // Confirm the account really is a Feed (owner checked above, discriminator here).
        Feed::try_from(*feed_account)?;

        let current_users = prior_seats
            .iter()
            .find(|s| s.feed_key == feed_key)
            .map(|s| s.current_users)
            .unwrap_or(0);

        // A zero current cap admits no users, which is not a meaningful seat to provision.
        if config.max_users == 0 {
            msg!("max_users must be > 0 for feed {}", feed_key);
            return Err(DoubleZeroError::FeedMaxUsersZero.into());
        }

        // A re-provision must not set the current cap below the live count carried over from the
        // prior seat, or the seat would start over its cap.
        if config.max_users < current_users {
            msg!(
                "max_users {} below current_users {} for feed {}",
                config.max_users,
                current_users,
                feed_key
            );
            return Err(DoubleZeroError::FeedMaxUsersBelowCurrentUsers.into());
        }

        // For now the future cap may only grow (or hold) relative to the current cap. Shrinking it
        // would force a decision about which live users to drop when the cap flips at window_end,
        // which is unspecified; requiring max_future_users >= max_users keeps the flip a no-op for
        // existing users. (Stricter than #1907; revisit if attrition-based reduction is wanted.)
        if config.max_future_users < config.max_users {
            msg!(
                "max_future_users {} below max_users {} for feed {}",
                config.max_future_users,
                config.max_users,
                feed_key
            );
            return Err(DoubleZeroError::FeedMaxFutureUsersBelowMaxUsers.into());
        }

        // anniversary_day is a calendar day-of-month; the granter clamps to the shortest month.
        if !(1..=31).contains(&config.anniversary_day) {
            msg!(
                "anniversary_day {} out of range 1..=31 for feed {}",
                config.anniversary_day,
                feed_key
            );
            return Err(DoubleZeroError::FeedInvalidAnniversaryDay.into());
        }

        // window_end and terminates_at are absolute unix seconds. window_end must still be in the
        // future (the cap flip hasn't already elapsed) and no later than terminates_at; since
        // window_end > now and window_end <= terminates_at, terminates_at is future too. This
        // rejects unset (0), inverted, and already-elapsed windows in one check.
        if config.window_end <= now || config.window_end > config.terminates_at {
            msg!(
                "invalid billing window for feed {}: now {}, window_end {}, terminates_at {}",
                feed_key,
                now,
                config.window_end,
                config.terminates_at
            );
            return Err(DoubleZeroError::FeedInvalidBillingWindow.into());
        }

        new_seats.push(FeedSeat {
            feed_key,
            max_users: config.max_users,
            max_future_users: config.max_future_users,
            current_users,
            anniversary_day: config.anniversary_day,
            window_end: config.window_end,
            terminates_at: config.terminates_at,
        });
    }

    accesspass.accesspass_type = AccessPassType::EdgeSeat(new_seats);
    try_acc_write(&accesspass, accesspass_account, payer_account, accounts)?;

    msg!("Set {} feed(s) on access pass", value.feeds.len());

    Ok(())
}
