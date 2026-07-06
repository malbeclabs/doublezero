use crate::{
    authorize::authorize,
    error::DoubleZeroError,
    pda::get_feed_pda,
    seeds::{SEED_FEED, SEED_PREFIX},
    serializer::try_acc_create,
    state::{
        accounttype::AccountType,
        feed::{Feed, MetroGroups},
        globalstate::GlobalState,
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
/// Maximum number of metro entries in a feed's metro map.
pub const MAX_FEED_METROS: usize = 64;
/// Maximum number of groups within a single metro entry.
pub const MAX_FEED_GROUPS_PER_METRO: usize = 64;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Debug, Clone, Default)]
pub struct FeedCreateArgs {
    pub code: String,
    pub name: String,
    /// `exchange_pk → group_pks`. Empty ⇒ no metro restriction.
    pub metros: Vec<MetroGroups>,
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

    validate_feed_inputs(&value.name, &value.metros)?;

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

    msg!("Created feed: {}", code);

    Ok(())
}

/// Validate the mutable feed inputs (`name` and the metro map) shared by create and update.
///
/// A duplicate exchange is rejected outright rather than silently deduped, since it implies
/// conflicting group sets and is almost certainly a mistake. A top-level empty `metros` vec is
/// valid (it means "no metro restriction"); only an empty `groups` vec *within* a metro is
/// degenerate (a covered metro with nothing joinable) and is rejected.
pub(crate) fn validate_feed_inputs(
    name: &str,
    metros: &[MetroGroups],
) -> Result<(), DoubleZeroError> {
    if name.len() > MAX_FEED_NAME_LEN {
        msg!("Feed name too long: {} > {}", name.len(), MAX_FEED_NAME_LEN);
        return Err(DoubleZeroError::NameTooLong);
    }
    if metros.len() > MAX_FEED_METROS {
        msg!("Too many metros: {} > {}", metros.len(), MAX_FEED_METROS);
        return Err(DoubleZeroError::InvalidArgument);
    }
    for (i, m) in metros.iter().enumerate() {
        if m.groups.is_empty() {
            msg!("Metro {} has no groups", m.exchange);
            return Err(DoubleZeroError::InvalidArgument);
        }
        if m.groups.len() > MAX_FEED_GROUPS_PER_METRO {
            msg!(
                "Metro {} has too many groups: {} > {}",
                m.exchange,
                m.groups.len(),
                MAX_FEED_GROUPS_PER_METRO
            );
            return Err(DoubleZeroError::InvalidArgument);
        }
        if metros[..i].iter().any(|prev| prev.exchange == m.exchange) {
            msg!("Duplicate exchange in metros: {}", m.exchange);
            return Err(DoubleZeroError::InvalidArgument);
        }
    }
    Ok(())
}
