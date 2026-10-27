use crate::{
    authorize::{authorize, FEED_ADMIN_FLAGS},
    error::DoubleZeroError,
    processors::feed::{
        check_groups_on_bond,
        create::{validate_feed_groups, validate_feed_name},
        split_group_accounts,
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
    pub groups: Option<Vec<Pubkey>>,
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

    assert!(payer_account.is_signer, "Payer must be a signer");
    assert_eq!(feed_account.owner, program_id, "Invalid PDA Account Owner");
    assert_eq!(
        globalstate_account.owner, program_id,
        "Invalid GlobalState Account Owner"
    );
    assert!(feed_account.is_writable, "PDA Account is not writable");

    let globalstate = GlobalState::try_from(globalstate_account)?;
    let mut feed = Feed::try_from(feed_account)?;
    let (group_accounts, authorize_candidates) = split_group_accounts(
        value.groups.as_deref().unwrap_or_default(),
        accounts_iter.collect(),
    );

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
        if let Some(ref groups) = value.groups {
            if !feed.groups.iter().all(|group| groups.contains(group)) {
                msg!("A feed's builder can add groups but not remove them");
                return Err(DoubleZeroError::NotAllowed.into());
            }
            // The groups already listed passed this check when they were added.
            let added = groups
                .iter()
                .filter(|group| !feed.groups.contains(group))
                .copied()
                .collect::<Vec<_>>();
            check_groups_on_bond(
                program_id,
                &feed.builder,
                &feed.bond_ref,
                &added,
                &group_accounts,
            )?;
        }
    } else {
        authorize(
            program_id,
            &mut authorize_candidates.into_iter(),
            payer_account.key,
            &globalstate,
            FEED_ADMIN_FLAGS,
        )?;
    }

    if let Some(ref name) = value.name {
        validate_feed_name(name)?;
    }
    if let Some(ref groups) = value.groups {
        validate_feed_groups(groups)?;
    }

    if let Some(ref name) = value.name {
        feed.name = name.clone();
    }
    if let Some(ref groups) = value.groups {
        feed.groups = groups.clone();
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

    msg!("Updated feed: {}", feed_account.key);

    Ok(())
}
