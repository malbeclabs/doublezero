use crate::{
    authorize::{authorize, FEED_ADMIN_FLAGS},
    error::DoubleZeroError,
    processors::{
        bond_mirror::split_bond_mirror,
        feed::{require_bond_still_covers, require_feed_writer},
    },
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

    // The bond mirror rides after the fixed accounts, found by its address rather than its
    // position, so a catalog feed's caller is not forced to send one.
    let (mirror_account, authorize_candidates) = split_bond_mirror(
        program_id,
        (feed.builder != Pubkey::default()).then_some(&feed.bond_ref),
        accounts_iter,
    );
    let mut authorize_iter = authorize_candidates.into_iter();

    // An operator's halt is not the builder's to lift. Letting the builder resume it would undo
    // the halt the moment it landed, and with `Retired` unreachable and `DeleteFeed` refusing a
    // bonded feed, nothing else stops one.
    let halted_by_operator = feed.halted_by != Pubkey::default() && feed.halted_by != feed.builder;
    if halted_by_operator {
        msg!("Feed {} was halted by {}", feed_account.key, feed.halted_by);
        authorize(
            program_id,
            &mut authorize_iter,
            payer_account.key,
            &globalstate,
            FEED_ADMIN_FLAGS,
        )?;
    } else {
        require_feed_writer(
            program_id,
            &mut authorize_iter,
            payer_account.key,
            &globalstate,
            &feed,
        )?;
    }

    if feed.status != FeedStatus::Halted {
        msg!(
            "Feed {} is {}, so there is nothing to resume",
            feed_account.key,
            feed.status
        );
        return Err(DoubleZeroError::FeedNotResumable.into());
    }

    // A halted feed's bond can be corrected downward while it sits, so publication resumes only
    // against cover that still holds. Checking here rather than trusting the check made when the
    // feed was created is the difference between a rule and a memory of one.
    if let Some(mirror_account) = mirror_account {
        require_bond_still_covers(program_id, mirror_account, feed_account.key, &feed)?;
    } else if feed.builder != Pubkey::default() {
        msg!(
            "Bonded feed {} needs its bond mirror to resume",
            feed_account.key
        );
        return Err(DoubleZeroError::BondMirrorMissing.into());
    }

    feed.status = FeedStatus::Active;
    feed.halted_by = Pubkey::default();
    try_acc_write(&feed, feed_account, payer_account, accounts)?;

    msg!("Resumed feed: {}", feed_account.key);

    Ok(())
}
