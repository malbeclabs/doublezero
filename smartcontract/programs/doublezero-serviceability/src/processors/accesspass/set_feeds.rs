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
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};
use std::net::Ipv4Addr;

/// Provision the feed_keys (SKU seats) onto an EdgeSeat access pass. The provisioning actor is the
/// oracle, authorized via its `ACCESS_PASS_ADMIN` Permission — not the deprecated `feed_authority`
/// slot. `current_users` is preserved for feeds already present on the pass; caps come from the
/// caller (seeded from the coupon).
#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone)]
pub struct SetAccessPassFeedsArgs {
    #[incremental(default = Ipv4Addr::UNSPECIFIED)]
    pub client_ip: Ipv4Addr,
    pub user_payer: Pubkey,
    pub feeds: Vec<FeedSeat>,
}

impl fmt::Debug for SetAccessPassFeedsArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "client_ip: {}, user_payer: {}, feeds: {}",
            self.client_ip,
            self.user_payer,
            self.feeds.len()
        )
    }
}

pub fn process_set_access_pass_feeds(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &SetAccessPassFeedsArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let accesspass_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    // One Feed account per requested seat, in the same order as `value.feeds`.
    let mut feed_accounts = Vec::with_capacity(value.feeds.len());
    for _ in 0..value.feeds.len() {
        feed_accounts.push(next_account_info(accounts_iter)?);
    }

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
    let prior_seats = accesspass.feed_seats().to_vec();

    // Validate each referenced Feed, preserve live counts, and bump reference_count for
    // newly-referenced feeds.
    let mut new_seats = Vec::with_capacity(value.feeds.len());
    for (seat, feed_account) in value.feeds.iter().zip(feed_accounts.iter()) {
        assert_eq!(
            *feed_account.key, seat.feed_key,
            "Feed account does not match seat feed_key"
        );
        assert_eq!(feed_account.owner, program_id, "Invalid Feed Account Owner");
        let mut feed = Feed::try_from(*feed_account)?;

        let current_users = prior_seats
            .iter()
            .find(|s| s.feed_key == seat.feed_key)
            .map(|s| s.current_users)
            .unwrap_or(0);

        if !prior_seats.iter().any(|s| s.feed_key == seat.feed_key) {
            assert!(feed_account.is_writable, "Feed Account is not writable");
            feed.reference_count = feed
                .reference_count
                .checked_add(1)
                .ok_or(DoubleZeroError::InvalidIndex)?;
            try_acc_write(&feed, feed_account, payer_account, accounts)?;
        }

        new_seats.push(FeedSeat {
            feed_key: seat.feed_key,
            max_users: seat.max_users,
            current_users,
        });
    }

    accesspass.accesspass_type = AccessPassType::EdgeSeat(new_seats);
    try_acc_write(&accesspass, accesspass_account, payer_account, accounts)?;

    msg!("Set {} feed(s) on access pass", value.feeds.len());

    Ok(())
}
