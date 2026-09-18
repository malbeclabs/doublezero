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
pub struct FeedHaltArgs {}

/// Stop a feed publishing, reversibly.
///
/// Halt is the builder's own lever and doubles as a way to rotate the upstream source without
/// redeploying, so it is reversible and touches neither the stake nor the seats already sold.
/// Retirement is the terminal path and is not this.
pub fn process_halt_feed(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    _value: &FeedHaltArgs,
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

    // Only a publishing feed can stop publishing. Pending has not started and Retired is terminal,
    // so neither is a halt this instruction can honor.
    if feed.status != FeedStatus::Active {
        msg!(
            "Feed {} is {}, so there is nothing to halt",
            feed_account.key,
            feed.status
        );
        return Err(DoubleZeroError::FeedNotHaltable.into());
    }

    feed.status = FeedStatus::Halted;
    // Recorded so resume can tell an operator's halt from the builder's own.
    feed.halted_by = *payer_account.key;
    try_acc_write(&feed, feed_account, payer_account, accounts)?;

    msg!("Halted feed: {}", feed_account.key);

    Ok(())
}
