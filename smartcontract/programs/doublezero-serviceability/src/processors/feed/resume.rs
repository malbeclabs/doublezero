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
    entrypoint::ProgramResult,
    msg,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Debug, Clone, Default)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct FeedResumeArgs {}

/// Put a halted feed back to publishing.
///
/// Only from `Halted`. Resuming a `Pending` feed would skip the conformance verdict it is waiting
/// on, and nothing leaves `Retired`.
pub fn process_resume_feed(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &FeedResumeArgs,
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

    if feed.status != FeedStatus::Halted {
        msg!(
            "Feed {} is {}, so there is nothing to resume",
            feed_account.key,
            feed.status
        );
        return Err(DoubleZeroError::FeedNotResumable.into());
    }

    feed.status = FeedStatus::Active;
    try_acc_write(&feed, feed_account, payer_account, accounts)?;

    msg!("Resumed feed: {}", feed_account.key);

    Ok(())
}
