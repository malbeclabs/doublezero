use crate::{
    authorize::authorize,
    error::DoubleZeroError,
    serializer::try_acc_close,
    state::{
        feed::{Feed, FeedStatus},
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

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Debug, Clone, Default)]
pub struct FeedDeleteArgs {}

pub fn process_delete_feed(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &FeedDeleteArgs,
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

    let globalstate = GlobalState::try_from(globalstate_account)?;
    authorize(
        program_id,
        accounts_iter,
        payer_account.key,
        &globalstate,
        permission_flags::FEED_AUTHORITY | permission_flags::FOUNDATION,
    )?;

    // Validate the account really is a Feed before closing it (guards against closing an unrelated
    // program-owned account). Feeds are not reference-counted: a still-referenced feed_key that is
    // deleted fails closed at connect (the metro gate can't load the deleted Feed), so deleting a
    // catalog feed is safe and the oracle owns keeping feeds and passes in sync.
    let feed = Feed::try_from(feed_account)?;

    // A staked feed is not a catalog entry an admin can drop. Its stake mirror records this feed
    // in `feed_key` to enforce RFC-28's one feed per stake, and closing the feed here would leave
    // that pointing at an account that no longer exists: the builder's bond would back nothing
    // and still refuse to back anything else. Retirement is the path out, and it releases the
    // stake with it. Both land in the feed lifecycle work (D2).
    if feed.builder != Pubkey::default() {
        msg!(
            "Feed {} is staked by builder {}; retire it instead",
            feed_account.key,
            feed.builder
        );
        return Err(DoubleZeroError::StakedFeedCannotBeDeleted.into());
    }

    // A feed under notice is not a catalog entry to drop either. Closing it here would end the
    // thirty days seat holders were promised, and the promise is the only thing `Retiring` means.
    // Finalize it first: `Retired` deletes like anything else.
    if feed.status == FeedStatus::Retiring {
        msg!(
            "Feed {} is retiring and cannot be deleted until its notice elapses",
            feed_account.key
        );
        return Err(DoubleZeroError::RetiringFeedCannotBeDeleted.into());
    }

    msg!("Deleted feed: {}", feed_account.key);

    try_acc_close(feed_account, payer_account)?;

    Ok(())
}
