use crate::{
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::feed::{Feed, FeedStatus},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    clock::Clock,
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
    sysvar::Sysvar,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Debug, Clone, Default)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct FeedFinalizeRetirementArgs {}

/// Move a feed from `Retiring` to `Retired` once its notice has elapsed.
///
/// Permissionless, unlike every other feed instruction. The outcome is fixed the moment retirement
/// starts: the clock decides, and this instruction can only agree with it. Requiring an authority
/// would add a way for a feed to sit in `Retiring` forever because whoever held the key stopped
/// caring, and a seat holder given a date deserves the date rather than someone's attention.
pub fn process_finalize_feed_retirement(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &FeedFinalizeRetirementArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let feed_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    assert!(payer_account.is_signer, "Payer must be a signer");
    assert_eq!(feed_account.owner, program_id, "Invalid PDA Account Owner");
    assert!(feed_account.is_writable, "PDA Account is not writable");

    let mut feed = Feed::try_from(feed_account)?;

    if feed.status != FeedStatus::Retiring {
        msg!(
            "Feed {} is {}, so it has no retirement to finish",
            feed_account.key,
            feed.status
        );
        return Err(DoubleZeroError::FeedNotRetiring.into());
    }

    let now = Clock::get()?.unix_timestamp;
    if now < feed.retires_at {
        msg!(
            "Feed {} retires at {}, and it is {}",
            feed_account.key,
            feed.retires_at,
            now
        );
        return Err(DoubleZeroError::RetirementNoticeNotElapsed.into());
    }

    feed.status = FeedStatus::Retired;
    try_acc_write(&feed, feed_account, payer_account, accounts)?;

    // The `FeedRetired` event, as a log line. This program has no event mechanism: no
    // `sol_log_data`, no return data, only `msg!`. Nothing consumes this yet, because releasing
    // the stake on retirement waits for the slashing work, so inventing a mechanism for a single
    // absent consumer would buy nothing. The shape is fixed so that whoever consumes it can parse
    // it without this line changing.
    msg!(
        "FeedRetired feed={} builder={} stake_ref={}",
        feed_account.key,
        feed.builder,
        feed.stake_ref
    );

    Ok(())
}
