use crate::{
    authorize::authorize,
    error::DoubleZeroError,
    serializer::try_acc_write,
    state::{
        feed::{Feed, FeedChain},
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
pub struct FeedMigrateArgs {
    pub feed_chain: FeedChain,
}

pub fn process_migrate_feed(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &FeedMigrateArgs,
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

    if value.feed_chain == FeedChain::Unspecified {
        msg!("MigrateFeed refuses unspecified");
        return Err(DoubleZeroError::InvalidFeedChain.into());
    }

    let mut feed = Feed::try_from(feed_account)?;
    if feed.feed_chain != FeedChain::Unspecified {
        msg!(
            "Feed {} already has chain {}",
            feed_account.key,
            feed.feed_chain
        );
        return Err(DoubleZeroError::FeedAlreadyMigrated.into());
    }

    feed.feed_chain = value.feed_chain;
    try_acc_write(&feed, feed_account, payer_account, accounts)?;

    msg!("Migrated feed: {}", feed_account.key);

    Ok(())
}
