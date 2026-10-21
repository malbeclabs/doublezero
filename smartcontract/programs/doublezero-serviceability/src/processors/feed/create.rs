use crate::{
    authorize::{authorize, FEED_ADMIN_FLAGS},
    error::{DoubleZeroError, Validate},
    pda::get_feed_pda,
    processors::bond_mirror::split_bond_mirror,
    seeds::{SEED_FEED, SEED_PREFIX},
    serializer::{try_acc_create, try_acc_write},
    state::{
        accounttype::AccountType,
        bond_mirror::BondMirror,
        feature_flags::{is_feature_enabled, FeatureFlag},
        feed::{Feed, FeedChain, FeedStatus},
        globalstate::GlobalState,
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
    /// behavior. Any other value makes this a bonded feed and starts it Pending.
    #[incremental(default = Pubkey::default())]
    pub builder: Pubkey,
    /// The `BuilderBond` PDA on Solana holding this feed's bond.
    #[incremental(default = Pubkey::default())]
    pub bond_ref: Pubkey,
    /// The `edge-feed-spec` wire format, as `<spec>@<version>`.
    #[incremental(default = String::new())]
    pub spec_id: String,
    /// SHA-256 of the declared service level.
    #[incremental(default = [0u8; 32])]
    pub sla_hash: [u8; 32],
    /// Committed rate in bits per second, `u64::MAX` for the unmetered tier.
    #[incremental(default = 0)]
    pub committed_rate_bits_per_sec: u64,
    #[incremental(default = FeedChain::Unspecified)]
    pub feed_chain: FeedChain,
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

    // The tail holds the bond's BondMirror, the payer's Permission account, both, or neither.
    // `value` rather than a `Feed` here, because this is describing a feed that does not exist yet.
    let (bond_mirror_account, authorize_candidates) = split_bond_mirror(
        program_id,
        (value.builder != Pubkey::default()).then_some(&value.bond_ref),
        accounts_iter,
    );
    let mut authorize_iter = authorize_candidates.into_iter();

    assert!(payer_account.is_signer, "Payer must be a signer");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert!(feed_account.is_writable, "PDA Account is not writable");

    // Two things admit this call: a builder's own bond, or the catalog permission. The bond goes
    // first, because RFC-28's claim is that a bond rather than an admin is what lets a builder
    // deploy. The catalog path below is untouched, so an admin creating a feed on a builder's
    // behalf works exactly as it did.
    let globalstate = GlobalState::try_from(globalstate_account)?;
    if !bond_authorizes(
        program_id,
        payer_account.key,
        value,
        bond_mirror_account,
        globalstate.feature_flags,
    ) {
        // Authorize before any input validation or existence probing so an unauthorized caller gets
        // NotAllowed rather than being able to trip validation errors or probe whether a feed exists.
        // Catalog admin: FEED_AUTHORITY (Permission PDA) or FOUNDATION.
        authorize(
            program_id,
            &mut authorize_iter,
            payer_account.key,
            &globalstate,
            FEED_ADMIN_FLAGS,
        )?;
    }

    validate_feed_name(&value.name)?;
    validate_feed_groups(&value.groups)?;
    validate_feed_bond_terms(value, globalstate.feature_flags)?;
    let claimed_mirror = if value.builder == Pubkey::default() {
        None
    } else {
        let mirror_account = bond_mirror_account.ok_or(DoubleZeroError::BondMirrorMissing)?;
        let mirror = verify_bond_covers_rate(program_id, mirror_account, value)?;
        Some((mirror_account, mirror))
    };
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
        bond_ref: value.bond_ref,
        spec_id: value.spec_id.clone(),
        sla_hash: value.sla_hash,
        committed_rate_bits_per_sec: value.committed_rate_bits_per_sec,
        // A bonded feed waits on a conformance verdict before it sells seats. A catalog feed has no
        // builder to attest, so it is sellable on creation, as it was before RFC-28.
        status: if value.builder == Pubkey::default() {
            FeedStatus::Active
        } else {
            FeedStatus::Pending
        },
        halted_by: Pubkey::default(),
        retires_at: 0,
        feed_chain: value.feed_chain,
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

    // Spend the bond on this feed. Ordering matters: the feed account is created first, so a
    // failure anywhere above leaves the bond unclaimed and the builder can retry.
    if let Some((mirror_account, mut mirror)) = claimed_mirror {
        mirror.feed_key = *feed_account.key;
        try_acc_write(&mirror, mirror_account, payer_account, accounts)?;
    }

    msg!("Created feed: {} @ {}", code, value.exchange);

    Ok(())
}

/// Whether a bond, rather than a catalog permission, admits this create.
///
/// Reads the mirror rather than trusting that one is there, and leaves only the tier question to
/// `verify_bond_covers_rate` below. That split is what lets a builder whose bond is too small be
/// told which, while a caller with no claim on any bond falls through to the permission check and
/// sees `NotAllowed`.
///
/// Every check here is on what the ledger holds. `builder` and `bond_ref` are both caller-supplied
/// arguments, and an earlier version of this gate took them at their word: it admitted anyone who
/// named themselves `builder` and attached the empty account that any `bond_ref` derives.
///
/// `&value.builder == payer` is the load-bearing line. `builder` is a caller-supplied argument, so
/// without it one builder's bond creates another builder's feed.
///
/// The feature-flag check is redundant today: `validate_feed_bond_terms` refuses any create
/// naming a builder while `allow-bonded-feeds` is clear, so dropping it here changes no outcome.
/// It stays because an authorization decision that is safe only because of a validation running
/// later is a trap for whoever edits either one next.
fn bond_authorizes(
    program_id: &Pubkey,
    payer: &Pubkey,
    value: &FeedCreateArgs,
    bond_mirror_account: Option<&AccountInfo>,
    feature_flags: u128,
) -> bool {
    if !is_feature_enabled(feature_flags, FeatureFlag::AllowBondedFeeds)
        || value.builder == Pubkey::default()
        || &value.builder != payer
    {
        return false;
    }

    // An account at the right address is not a mirror. The address is derived from `bond_ref`,
    // which is a caller-supplied argument, so anyone can name a bond nobody posted and attach the
    // empty account its seeds derive. Reading it is what makes this a bond rather than an
    // assertion that one exists.
    let Some(account) = bond_mirror_account else {
        return false;
    };
    if account.data_is_empty() || account.owner != program_id {
        return false;
    }
    let Ok(mirror) = BondMirror::try_from(account) else {
        return false;
    };
    if mirror.validate().is_err() {
        return false;
    }

    // The builder the mirror records, not the one the caller claimed. `value.builder` is an
    // argument; this is what the relayer wrote.
    &mirror.builder == payer
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

/// Validate the RFC-28 bond terms on create. The terms travel together: a bonded feed names a
/// builder, the bond behind it, the spec it conforms to, and the rate it commits to. A feed with
/// some of those and not the others is a half-declared feed that nothing downstream can measure.
///
/// This does not check that the bond covers the rate. That check reads `BondMirror` and lands
/// with it.
pub(crate) fn validate_feed_bond_terms(
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
        && !is_feature_enabled(feature_flags, FeatureFlag::AllowBondedFeeds)
    {
        msg!("Bonded feeds are not enabled on this cluster");
        return Err(DoubleZeroError::NotAllowed);
    }

    if value.builder == Pubkey::default() {
        // Catalog feed. It carries no bond terms at all.
        if value.bond_ref != Pubkey::default()
            || !value.spec_id.is_empty()
            || value.sla_hash != [0u8; 32]
            || value.committed_rate_bits_per_sec != 0
        {
            msg!("Feed bond terms given without a builder");
            return Err(DoubleZeroError::InvalidArgument);
        }
        return Ok(());
    }

    if value.bond_ref == Pubkey::default() {
        msg!("Bonded feed must name the bond account behind it");
        return Err(DoubleZeroError::InvalidArgument);
    }
    if value.spec_id.is_empty() {
        msg!("Bonded feed must name the edge-feed-spec it conforms to");
        return Err(DoubleZeroError::InvalidArgument);
    }
    if value.sla_hash == [0u8; 32] {
        msg!("Bonded feed must declare a service level");
        return Err(DoubleZeroError::InvalidArgument);
    }
    if value.committed_rate_bits_per_sec == 0 {
        msg!("Bonded feed must commit to a rate");
        return Err(DoubleZeroError::InvalidArgument);
    }
    // The rewards process uses the chain or the builder of a feed to find who gets the rewards.
    // A feed with both would not show clearly who gets them.
    if value.feed_chain != FeedChain::Unspecified {
        msg!("Bonded feed must not name a chain");
        return Err(DoubleZeroError::InvalidArgument);
    }

    Ok(())
}

/// Check the builder's mirrored bond against the rate this feed commits to.
///
/// The mirror is written by a relayer watching `builder-bond` on Solana, so this is a check
/// against an assertion rather than against the Solana account itself. An unwritten mirror decodes
/// as tier `None`, which covers no rate, so "no bond yet" and "bond too small" fail the same way
/// and neither can slip through.
fn verify_bond_covers_rate(
    program_id: &Pubkey,
    mirror_account: &AccountInfo,
    value: &FeedCreateArgs,
) -> Result<BondMirror, DoubleZeroError> {
    if !mirror_account.is_writable {
        msg!("Bond mirror must be writable so the feed can claim the bond");
        return Err(DoubleZeroError::InvalidArgument);
    }
    if mirror_account.data_is_empty() || mirror_account.owner != program_id {
        msg!("No bond mirror written for bond {}", value.bond_ref);
        return Err(DoubleZeroError::BondDoesNotCoverRate);
    }

    let mirror =
        BondMirror::try_from(mirror_account).map_err(|_| DoubleZeroError::InvalidAccountType)?;
    // A mirror that fails its own invariants is a half-written account, not a bond. Check it
    // before trusting the tier it asserts.
    mirror.validate()?;

    // The PDA seed already binds the account to `bond_ref`. These catch a mirror whose stored keys
    // disagree with its own address, which would mean the writer got it wrong.
    if mirror.bond_ref != value.bond_ref {
        msg!("Bond mirror names a different bond: {}", mirror.bond_ref);
        return Err(DoubleZeroError::InvalidArgument);
    }
    if mirror.builder != value.builder {
        msg!("Bond mirror names a different builder: {}", mirror.builder);
        return Err(DoubleZeroError::InvalidArgument);
    }

    // RFC-28: one feed per bond, so slashing one feed never reaches another.
    if mirror.feed_key != Pubkey::default() {
        msg!("Bond already backs feed {}", mirror.feed_key);
        return Err(DoubleZeroError::BondAlreadyBacksFeed);
    }

    if !mirror.tier.covers(value.committed_rate_bits_per_sec) {
        msg!(
            "Tier {} covers up to {} bits/sec, feed commits to {}",
            mirror.tier,
            mirror.tier.max_rate_bits_per_sec(),
            value.committed_rate_bits_per_sec
        );
        return Err(DoubleZeroError::BondDoesNotCoverRate);
    }

    Ok(mirror)
}
