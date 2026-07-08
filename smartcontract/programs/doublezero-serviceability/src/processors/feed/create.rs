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
use doublezero_program_common::validate_account_code;
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

/// Maximum `name` length, matching the Exchange/Location `name` cap.
pub const MAX_FEED_NAME_LEN: usize = 64;
/// Maximum number of multicast groups in a feed.
pub const MAX_FEED_GROUPS: usize = 64;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Debug, Clone, Default)]
pub struct FeedCreateArgs {
    pub code: String,
    pub name: String,
    /// The metro (exchange) this feed serves; part of the PDA seed.
    pub exchange: Pubkey,
    /// Multicast groups joinable in this metro.
    pub groups: Vec<Pubkey>,
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
    assert!(feed_account.is_writable, "PDA Account is not writable");

    // Authorize before any input validation or existence probing so an unauthorized caller gets
    // NotAllowed rather than being able to trip validation errors or probe whether a feed exists.
    // Catalog admin: FEED_AUTHORITY (Permission PDA) or FOUNDATION.
    let globalstate = GlobalState::try_from(globalstate_account)?;
    authorize(
        program_id,
        accounts_iter,
        payer_account.key,
        &globalstate,
        permission_flags::FEED_AUTHORITY | permission_flags::FOUNDATION,
    )?;

    validate_feed_name(&value.name)?;
    validate_feed_groups(&value.groups)?;
    // Every feed is scoped to a real metro; there is no metro-agnostic feed.
    if value.exchange == Pubkey::default() {
        msg!("Feed exchange must be a real metro, not the default pubkey");
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    let code =
        validate_account_code(&value.code).map_err(|_| DoubleZeroError::InvalidAccountCode)?;

    let (expected_pda, bump_seed) = get_feed_pda(program_id, &code, &value.exchange);
    assert_eq!(feed_account.key, &expected_pda, "Invalid Feed PubKey");

    if !feed_account.data_is_empty() {
        return Err(ProgramError::AccountAlreadyInitialized);
    }

    let feed = Feed {
        account_type: AccountType::Feed,
        owner: *payer_account.key,
        bump_seed,
        code: code.clone(),
        name: value.name.clone(),
        reference_count: 0,
        exchange: value.exchange,
        groups: value.groups.clone(),
    };

    try_acc_create(
        &feed,
        feed_account,
        payer_account,
        system_program,
        program_id,
        &[
            SEED_PREFIX,
            SEED_FEED,
            code.as_bytes(),
            value.exchange.as_ref(),
            &[bump_seed],
        ],
    )?;

    msg!("Created feed: {} @ {}", code, value.exchange);

    Ok(())
}

/// Validate a feed `name`, shared by create and update.
pub(crate) fn validate_feed_name(name: &str) -> Result<(), DoubleZeroError> {
    if name.len() > MAX_FEED_NAME_LEN {
        msg!("Feed name too long: {} > {}", name.len(), MAX_FEED_NAME_LEN);
        return Err(DoubleZeroError::NameTooLong);
    }
    Ok(())
}

/// Validate a feed's multicast `groups`, shared by create and update. A feed must join at least one
/// group (an empty set is degenerate — nothing to connect to), stay within the size cap, and carry
/// no duplicate group.
pub(crate) fn validate_feed_groups(groups: &[Pubkey]) -> Result<(), DoubleZeroError> {
    if groups.is_empty() {
        msg!("Feed must have at least one group");
        return Err(DoubleZeroError::InvalidArgument);
    }
    if groups.len() > MAX_FEED_GROUPS {
        msg!("Too many groups: {} > {}", groups.len(), MAX_FEED_GROUPS);
        return Err(DoubleZeroError::InvalidArgument);
    }
    for (i, g) in groups.iter().enumerate() {
        if groups[..i].contains(g) {
            msg!("Duplicate group in feed: {}", g);
            return Err(DoubleZeroError::InvalidArgument);
        }
    }
    Ok(())
}
