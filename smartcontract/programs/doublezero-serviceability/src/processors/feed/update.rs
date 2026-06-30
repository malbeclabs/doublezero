use crate::{
    authorize::authorize,
    serializer::try_acc_write,
    state::{feed::Feed, globalstate::GlobalState, permission::permission_flags},
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    pubkey::Pubkey,
};

/// `code` is the PDA seed and therefore immutable; only `name` and the metro map are mutable.
#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct FeedUpdateArgs {
    pub name: Option<String>,
    pub metros: Option<Vec<(Pubkey, Vec<Pubkey>)>>,
}

impl fmt::Debug for FeedUpdateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "name: {:?}, metros: {:?}",
            self.name,
            self.metros.as_ref().map(|m| m.len())
        )
    }
}

pub fn process_update_feed(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &FeedUpdateArgs,
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

    let mut feed = Feed::try_from(feed_account)?;
    if let Some(ref name) = value.name {
        feed.name = name.clone();
    }
    if let Some(ref metros) = value.metros {
        feed.metros = metros.clone();
    }

    try_acc_write(&feed, feed_account, payer_account, accounts)?;

    Ok(())
}
