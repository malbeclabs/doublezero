use crate::{
    authorize::authorize,
    error::DoubleZeroError,
    pda::get_stake_mirror_pda,
    processors::feed::require_stake_still_covers,
    serializer::try_acc_write,
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
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct FeedActivateArgs {}

/// Admit a feed that was waiting on a conformance verdict.
///
/// Only from `Pending`. `Halted` has its own way back through `ResumeFeed`, and nothing leaves
/// `Retired`.
///
/// The args carry no verdict. Nothing reads one: the grounds a verdict would carry are what
/// programmatic slashing acts on, and no slash instruction exists to act on them. Recording a
/// payload here before anything consumes it would fix its shape at the point we know least about
/// it. What the caller's signature asserts is the whole of the verdict for now.
///
/// The builder cannot sign this, unlike halt and resume, which RFC-28 makes its own levers. A
/// builder that could admit its own feed would be attesting to its own conformance, which is the
/// one thing the Pending state exists to prevent.
pub fn process_activate_feed(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &FeedActivateArgs,
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

    // The stake mirror rides after the fixed accounts, found by its address rather than its
    // position, so a catalog feed's caller is not forced to send one.
    let tail: Vec<&AccountInfo> = accounts_iter.collect();
    let mirror_key = (feed.builder != Pubkey::default())
        .then(|| get_stake_mirror_pda(program_id, &feed.stake_ref).0);
    let mirror_account = mirror_key.and_then(|k| tail.iter().copied().find(|a| a.key == &k));
    let mut authorize_iter = tail.iter().copied().filter(|a| Some(*a.key) != mirror_key);

    authorize(
        program_id,
        &mut authorize_iter,
        payer_account.key,
        &globalstate,
        permission_flags::FEED_AUTHORITY | permission_flags::FOUNDATION,
    )?;

    if feed.status != FeedStatus::Pending {
        msg!(
            "Feed {} is {}, so there is no verdict outstanding",
            feed_account.key,
            feed.status
        );
        return Err(DoubleZeroError::FeedNotActivatable.into());
    }

    // This is the only place a corrected mirror reaches a feed that a bad one already admitted.
    // `CreateFeed` checked the cover it was shown; a relayer can write a smaller tier afterwards,
    // and between creation and this step is the whole window in which that correction can land
    // before the feed ever publishes.
    if let Some(mirror_account) = mirror_account {
        require_stake_still_covers(program_id, mirror_account, feed_account.key, &feed)?;
    } else if feed.builder != Pubkey::default() {
        msg!(
            "Staked feed {} needs its stake mirror to activate",
            feed_account.key
        );
        return Err(DoubleZeroError::StakeMirrorMissing.into());
    }

    feed.status = FeedStatus::Active;
    try_acc_write(&feed, feed_account, payer_account, accounts)?;

    msg!("Activated feed: {}", feed_account.key);

    Ok(())
}
