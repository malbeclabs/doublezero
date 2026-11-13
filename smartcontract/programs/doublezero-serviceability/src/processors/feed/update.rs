use crate::{
    authorize::{authorize, FEED_ADMIN_FLAGS},
    error::DoubleZeroError,
    processors::feed::{
        add_feed_to_groups, check_groups_on_bond,
        create::{validate_feed_groups, validate_feed_name},
        group_keys, next_group_accounts, remove_feed_from_groups,
    },
    serializer::try_acc_write,
    state::{
        feature_flags::{is_feature_enabled, FeatureFlag},
        feed::{Feed, FeedChain, FeedStatus},
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

/// `code` and `exchange` are the PDA seeds and therefore immutable; `name`, the group set, and
/// the chain are mutable.
#[derive(BorshSerialize, BorshDeserializeIncremental, PartialEq, Debug, Clone, Default)]
pub struct FeedUpdateArgs {
    pub name: Option<String>,
    /// The number of groups the feed adds. Their accounts follow the system program.
    pub added_group_count: u8,
    /// The number of groups the feed removes. Their accounts follow the added groups.
    pub removed_group_count: u8,
    /// The hash of the feed's groups as the caller read them. A group change fails when the feed's
    /// groups changed since then.
    pub expected_groups_hash: [u8; 32],
    #[incremental(default = None)]
    pub feed_chain: Option<FeedChain>,
}

pub fn process_update_feed(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    value: &FeedUpdateArgs,
) -> ProgramResult {
    if value == &FeedUpdateArgs::default() {
        msg!("UpdateFeed with no fields set");
        return Err(DoubleZeroError::InvalidArgument.into());
    }

    let accounts_iter = &mut accounts.iter();

    let feed_account = next_account_info(accounts_iter)?;
    let globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;
    let _system_program = next_account_info(accounts_iter)?;
    let added_accounts = next_group_accounts(accounts_iter, value.added_group_count)?;
    let removed_accounts = next_group_accounts(accounts_iter, value.removed_group_count)?;
    let added = group_keys(&added_accounts);
    let removed = group_keys(&removed_accounts);

    assert!(payer_account.is_signer, "Payer must be a signer");
    assert_eq!(feed_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert!(feed_account.is_writable, "PDA Account is not writable");

    let globalstate = GlobalState::load(program_id, globalstate_account)?;
    let mut feed = Feed::try_from(feed_account)?;

    // A bonded feed's own builder may rename it and add groups, held to the same rule as at
    // create: every group it adds must be the builder's own. It may not remove one: a subscriber's
    // leave drops only the groups the feed lists then, so a removed group would stay with every
    // subscriber who leaves after it. Anyone else needs the catalog permission, as before.
    let builder_signs = feed.builder != Pubkey::default()
        && &feed.builder == payer_account.key
        && is_feature_enabled(globalstate.feature_flags, FeatureFlag::AllowBondedFeeds);
    if builder_signs {
        if value.feed_chain.is_some() {
            msg!("A feed's builder cannot set its chain");
            return Err(DoubleZeroError::NotAllowed.into());
        }
        if matches!(feed.status, FeedStatus::Retiring | FeedStatus::Retired) {
            msg!("A retiring or retired feed's groups cannot change");
            return Err(DoubleZeroError::NotAllowed.into());
        }
        if !removed.is_empty() {
            msg!("A feed's builder can add groups but not remove them");
            return Err(DoubleZeroError::NotAllowed.into());
        }
        check_groups_on_bond(
            program_id,
            &feed.builder,
            &feed.bond_ref,
            &added,
            &added_accounts,
        )?;
    } else {
        authorize(
            program_id,
            accounts_iter,
            payer_account.key,
            &globalstate,
            FEED_ADMIN_FLAGS,
        )?;
    }

    if let Some(ref name) = value.name {
        validate_feed_name(name)?;
    }
    if (!added.is_empty() || !removed.is_empty())
        && value.expected_groups_hash != Feed::groups_hash(&feed.groups)
    {
        msg!(
            "Feed {} changed its groups after the caller read them. Read the feed again",
            feed_account.key
        );
        return Err(DoubleZeroError::FeedGroupsChanged.into());
    }
    for (i, group) in removed.iter().enumerate() {
        if removed[..i].contains(group) {
            msg!("Group {} is removed twice", group);
            return Err(DoubleZeroError::InvalidArgument.into());
        }
        if !feed.groups.contains(group) {
            msg!("Feed does not list group {}", group);
            return Err(DoubleZeroError::InvalidArgument.into());
        }
    }
    if let Some(group) = added.iter().find(|group| feed.groups.contains(group)) {
        msg!("Feed already lists group {}", group);
        return Err(DoubleZeroError::InvalidArgument.into());
    }
    if !added.is_empty() || !removed.is_empty() {
        feed.groups.retain(|group| !removed.contains(group));
        feed.groups.extend(&added);
        validate_feed_groups(&feed.groups)?;
    }

    if let Some(ref name) = value.name {
        feed.name = name.clone();
    }
    if let Some(feed_chain) = value.feed_chain {
        // The rewards process uses the chain or the builder of a feed to find who gets the
        // rewards. A feed with both would not show clearly who gets them.
        if feed.builder != Pubkey::default() && feed_chain != FeedChain::Unspecified {
            msg!("Bonded feed must not name a chain");
            return Err(DoubleZeroError::InvalidArgument.into());
        }
        feed.feed_chain = feed_chain;
    }

    try_acc_write(&feed, feed_account, payer_account, accounts)?;
    remove_feed_from_groups(program_id, &removed_accounts, payer_account, accounts)?;
    add_feed_to_groups(program_id, &added_accounts, payer_account, accounts)?;

    msg!("Updated feed: {}", feed_account.key);

    Ok(())
}
