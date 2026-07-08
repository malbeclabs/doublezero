use crate::{
    authorize::authorize,
    error::DoubleZeroError,
    serializer::try_acc_close,
    state::{feed::Feed, globalstate::GlobalState, permission::permission_flags},
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

    let feed = Feed::try_from(feed_account)?;
    if feed.reference_count > 0 {
        msg!(
            "Cannot delete feed: reference_count of {} > 0",
            feed.reference_count
        );
        return Err(DoubleZeroError::ReferenceCountNotZero.into());
    }

    msg!("Deleted feed: {}", feed_account.key);

    try_acc_close(feed_account, payer_account)?;

    Ok(())
}
