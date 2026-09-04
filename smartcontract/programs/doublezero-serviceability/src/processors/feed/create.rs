use crate::{
    authorize::authorize,
    error::DoubleZeroError,
    pda::get_feed_pda,
    seeds::{SEED_FEED, SEED_PREFIX},
    serializer::try_acc_create,
    state::{
        accounttype::AccountType,
        feature_flags::{is_feature_enabled, FeatureFlag},
        feed::{Feed, FeedStatus},
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
/// Maximum number of multicast groups in a feed. A feed's whole group set joins in one
/// `SubscribeFeed` transaction, so this is bounded by transaction capacity.
pub const MAX_FEED_GROUPS: usize = 20;
/// Maximum `spec_id` length. Holds `<spec>@<version>` for any name `edge-feed-spec` publishes.
pub const MAX_FEED_SPEC_ID_LEN: usize = 64;

#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Debug, Clone, Default)]
pub struct FeedCreateArgs {
    pub code: String,
    pub name: String,
    /// The metro (exchange) this feed serves; part of the PDA seed.
    pub exchange: Pubkey,
    /// Multicast groups joinable in this metro.
    pub groups: Vec<Pubkey>,

    // RFC-28 tail. A client built before RFC-28 sends nothing past `groups` and still creates a
    // catalog feed, which is what the defaults below describe.
    /// The builder deploying this feed. Zero creates a catalog feed with no builder, the pre-RFC-28
    /// behavior. Any other value makes this a staked feed and starts it Pending.
    #[incremental(default = Pubkey::default())]
    pub builder: Pubkey,
    /// The `BuilderStake` PDA on Solana holding this feed's deposit.
    #[incremental(default = Pubkey::default())]
    pub stake_ref: Pubkey,
    /// The `edge-feed-spec` wire format, as `<spec>@<version>`.
    #[incremental(default = String::new())]
    pub spec_id: String,
    /// SHA-256 of the declared service level.
    #[incremental(default = [0u8; 32])]
    pub sla_hash: [u8; 32],
    /// Committed rate in bits per second, `u64::MAX` for the unmetered tier.
    #[incremental(default = 0)]
    pub committed_rate_bits_per_sec: u64,
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
    validate_feed_stake_terms(value, globalstate.feature_flags)?;
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
        exchange: value.exchange,
        groups: value.groups.clone(),
        builder: value.builder,
        stake_ref: value.stake_ref,
        spec_id: value.spec_id.clone(),
        sla_hash: value.sla_hash,
        committed_rate_bits_per_sec: value.committed_rate_bits_per_sec,
        // A staked feed waits on a conformance verdict before it sells seats. A catalog feed has no
        // builder to attest, so it is sellable on creation, as it was before RFC-28.
        status: if value.builder == Pubkey::default() {
            FeedStatus::Active
        } else {
            FeedStatus::Pending
        },
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

/// Validate the RFC-28 stake terms on create. The terms travel together: a staked feed names a
/// builder, the stake behind it, the spec it conforms to, and the rate it commits to. A feed with
/// some of those and not the others is a half-declared feed that nothing downstream can measure.
///
/// This does not check that the stake covers the rate. That check reads `StakeMirror` and lands
/// with it.
pub(crate) fn validate_feed_stake_terms(
    value: &FeedCreateArgs,
    feature_flags: u128,
) -> Result<(), DoubleZeroError> {
    if value.spec_id.len() > MAX_FEED_SPEC_ID_LEN {
        msg!(
            "Feed spec_id too long: {} > {}",
            value.spec_id.len(),
            MAX_FEED_SPEC_ID_LEN
        );
        return Err(DoubleZeroError::InvalidArgument);
    }

    if value.builder != Pubkey::default()
        && !is_feature_enabled(feature_flags, FeatureFlag::AllowStakedFeeds)
    {
        msg!("Staked feeds are not enabled on this cluster");
        return Err(DoubleZeroError::NotAllowed);
    }

    if value.builder == Pubkey::default() {
        // Catalog feed. It carries no stake terms at all.
        if value.stake_ref != Pubkey::default()
            || !value.spec_id.is_empty()
            || value.sla_hash != [0u8; 32]
            || value.committed_rate_bits_per_sec != 0
        {
            msg!("Feed stake terms given without a builder");
            return Err(DoubleZeroError::InvalidArgument);
        }
        return Ok(());
    }

    if value.stake_ref == Pubkey::default() {
        msg!("Staked feed must name the stake account behind it");
        return Err(DoubleZeroError::InvalidArgument);
    }
    if value.spec_id.is_empty() {
        msg!("Staked feed must name the edge-feed-spec it conforms to");
        return Err(DoubleZeroError::InvalidArgument);
    }
    if value.sla_hash == [0u8; 32] {
        msg!("Staked feed must declare a service level");
        return Err(DoubleZeroError::InvalidArgument);
    }
    if value.committed_rate_bits_per_sec == 0 {
        msg!("Staked feed must commit to a rate");
        return Err(DoubleZeroError::InvalidArgument);
    }

    Ok(())
}
