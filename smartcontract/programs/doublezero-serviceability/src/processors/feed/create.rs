use crate::{
    authorize::authorize,
    error::DoubleZeroError,
    pda::get_feed_pda,
    seeds::{SEED_FEED, SEED_PREFIX},
    serializer::try_acc_create,
    state::{
        accounttype::AccountType, feed::Feed, globalstate::GlobalState,
        permission::permission_flags,
    },
};
use borsh::BorshSerialize;
use borsh_incremental::BorshDeserializeIncremental;
use core::fmt;
use doublezero_program_common::validate_account_code;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Clone, Default)]
pub struct FeedCreateArgs {
    pub code: String,
    pub name: String,
    /// `exchange_pk → group_pks`. Empty ⇒ no metro restriction.
    pub metros: Vec<(Pubkey, Vec<Pubkey>)>,
}

impl fmt::Debug for FeedCreateArgs {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "code: {}, name: {}, metros: {}",
            self.code,
            self.name,
            self.metros.len()
        )
    }
}

pub fn process_create_feed(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &FeedCreateArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let feed_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let system_program = next_account_info(accounts_iter)?;

    assert!(payer_account.is_signer, "Payer must be a signer");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert_eq!(
        *system_program.unsigned_key(),
        solana_system_interface::program::ID,
        "Invalid System Program Account Owner"
    );
    assert!(feed_account.is_writable, "PDA Account is not writable");

    let code =
        validate_account_code(&value.code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;

    let (expected_pda, bump_seed) = get_feed_pda(program_id, &code);
    assert_eq!(feed_account.key, &expected_pda, "Invalid Feed PubKey");

    if !feed_account.data_is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    // Catalog admin: FEED_AUTHORITY (Permission PDA) or FOUNDATION.
    let globalstate = GlobalState::try_from(globalstate_account)?;
    authorize(
        program_id,
        accounts_iter,
        payer_account.key,
        &globalstate,
        permission_flags::FEED_AUTHORITY | permission_flags::FOUNDATION,
    )?;

    let feed = Feed {
        account_type: AccountType::Feed,
        owner: *payer_account.key,
        bump_seed,
        code: code.clone(),
        name: value.name.clone(),
        reference_count: 0,
        metros: value.metros.clone(),
    };

    try_acc_create(
        &feed,
        feed_account,
        payer_account,
        system_program,
        program_id,
        &[SEED_PREFIX, SEED_FEED, code.as_bytes(), &[bump_seed]],
    )?;

    Ok(())
}
