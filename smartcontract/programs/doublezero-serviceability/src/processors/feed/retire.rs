use crate::{
    error::DoubleZeroError,
    processors::feed::require_feed_writer,
    serializer::try_acc_write,
    state::{
        feed::{Feed, FeedStatus},
        globalstate::GlobalState,
    },
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

/// The notice a feed owes its seat holders before publication stops.
pub const RETIREMENT_NOTICE_SECONDS: i64 = 30 * 24 * 60 * 60;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Debug, Clone, Default)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct FeedRetireArgs {}

/// Start a feed's retirement and the notice that runs with it.
///
/// One way in and no way back: `Retiring` is not resumable, and the only state after it is
/// `Retired`. That is what makes retirement terminal where halt is reversible.
pub fn process_retire_feed(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &FeedRetireArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let feed_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;

    assert!(payer_account.is_signer, "Payer must be a signer");
    assert_eq!(feed_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert!(feed_account.is_writable, "PDA Account is not writable");

    let mut feed = Feed::try_from(feed_account)?;
    let globalstate = GlobalState::try_from(globalstate_account)?;
    require_feed_writer(
        program_id,
        accounts_iter,
        payer_account.key,
        &globalstate,
        &feed,
    )?;

    // A feed already retiring or retired has nothing left to start. Everything else can retire,
    // including `Pending`: `DeleteFeed` refuses a staked feed, so refusing here as well would
    // leave a feed that never went live with no way out at all.
    if matches!(feed.status, FeedStatus::Retiring | FeedStatus::Retired) {
        msg!(
            "Feed {} is {}, so its retirement has already started",
            feed_account.key,
            feed.status
        );
        return Err(DoubleZeroError::FeedNotRetirable.into());
    }

    let now = Clock::get()?.unix_timestamp;

    // The notice exists for seat holders, and a feed that was never `Active` has none: it admits
    // no subscriber until it publishes. So the wait is zero rather than absent, which keeps one
    // path through retirement instead of two.
    feed.retires_at = if feed.status == FeedStatus::Pending {
        now
    } else {
        now.checked_add(RETIREMENT_NOTICE_SECONDS)
            .ok_or(DoubleZeroError::InvalidArgument)?
    };
    feed.status = FeedStatus::Retiring;

    try_acc_write(&feed, feed_account, payer_account, accounts)?;

    msg!(
        "Retiring feed: {} notice ends at {}",
        feed_account.key,
        feed.retires_at
    );

    Ok(())
}
