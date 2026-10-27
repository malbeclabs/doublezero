//! Feed-domain instruction builders.
//!
//! All route through `authorize()` -> [`common::build_with_permission`].
//! Accounts are `[feed, globalstate]`; create derives the feed PDA from
//! `args.code` and `args.exchange`.
//!
//! **Why `build_with_permission`.** The feed `create`/`update`/`delete`
//! processors call `authorize(.., FEED_AUTHORITY | FOUNDATION)`, so feed is a
//! permission-gated instruction like topology/tenant and belongs on the
//! permission-appending path: every builder here except `finalize_feed_retirement`
//! appends the payer's Permission PDA last. A bonded feed's bond mirror rides
//! between the system program and the Permission account.

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_bond_mirror_pda, get_feed_pda, get_globalstate_pda},
    processors::feed::{
        activate::FeedActivateArgs, create::FeedCreateArgs, delete::FeedDeleteArgs,
        finalize_retirement::FeedFinalizeRetirementArgs, halt::FeedHaltArgs,
        resume::FeedResumeArgs, retire::FeedRetireArgs, update::FeedUpdateArgs,
    },
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// `CreateFeed` (variant 112). Accounts: `[feed, globalstate]`.
///
/// The feed PDA is derived from `args.code` and `args.exchange`.
///
/// A builder's create carries each group twice, once in the data and once as an account, so it fits
/// in one transaction with at most nine groups. A builder with more creates the feed with nine and
/// adds the rest with `update_feed_as_builder`.
pub fn create_feed(program_id: &Pubkey, payer: &Pubkey, args: FeedCreateArgs) -> Instruction {
    let (feed, _) = get_feed_pda(program_id, &args.code, &args.exchange);
    let (globalstate, _) = get_globalstate_pda(program_id);

    // A bonded feed needs its bond mirror: `CreateFeed` reads the tier from it and writes the
    // feed's key onto it to claim the bond. Derived rather than taken as a parameter, because
    // unlike `resume_feed` the args already carry the bond it is seeded on, so asking a caller
    // for it would only create a way to get it wrong. Writable: creating the feed claims it.
    let bond_mirror = (args.builder != Pubkey::default())
        .then(|| get_bond_mirror_pda(program_id, &args.bond_ref).0);
    // A bonded feed its builder creates carries its groups' accounts, so the program can check
    // that each is one the builder created on its bond. An admin's create and a catalog feed pass
    // none: the program does not read them there, and at the group limit they would not fit in a
    // transaction.
    let mut after_system: Vec<AccountMeta> = bond_mirror
        .map(|bond_mirror| AccountMeta::new(bond_mirror, false))
        .into_iter()
        .collect();
    if args.builder != Pubkey::default() && args.builder == *payer {
        after_system.extend(group_accounts(&args.groups));
    }

    common::build_with_permission_after_system(
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        vec![
            AccountMeta::new(feed, false),
            AccountMeta::new(globalstate, false),
        ],
        after_system,
        payer,
    )
}

/// `UpdateFeed` (variant 113), as an admin sends it. Accounts: `[feed, globalstate]`.
pub fn update_feed(
    program_id: &Pubkey,
    payer: &Pubkey,
    feed: &Pubkey,
    args: FeedUpdateArgs,
) -> Instruction {
    update_feed_with_groups(program_id, payer, feed, args, Vec::new())
}

/// `UpdateFeed` (variant 113), as a bonded feed's builder sends it. Accounts: `[feed,
/// globalstate]`, then the account of each group `args` adds to `current_groups`, the feed's
/// groups now. The program checks that each added group is the builder's own, on the feed's bond,
/// and refuses an update that removes one.
pub fn update_feed_as_builder(
    program_id: &Pubkey,
    builder: &Pubkey,
    feed: &Pubkey,
    current_groups: &[Pubkey],
    args: FeedUpdateArgs,
) -> Instruction {
    let added = args
        .groups
        .iter()
        .flatten()
        .filter(|group| !current_groups.contains(group))
        .copied()
        .collect::<Vec<_>>();
    update_feed_with_groups(program_id, builder, feed, args, group_accounts(&added))
}

fn update_feed_with_groups(
    program_id: &Pubkey,
    payer: &Pubkey,
    feed: &Pubkey,
    args: FeedUpdateArgs,
    after_system: Vec<AccountMeta>,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission_after_system(
        program_id,
        DoubleZeroInstruction::UpdateFeed(args),
        vec![
            AccountMeta::new(*feed, false),
            AccountMeta::new(globalstate, false),
        ],
        after_system,
        payer,
    )
}

// Read-only: the program reads who owns each group and on which bond, and writes nothing to it.
fn group_accounts(groups: &[Pubkey]) -> Vec<AccountMeta> {
    groups
        .iter()
        .map(|group| AccountMeta::new_readonly(*group, false))
        .collect()
}

/// `DeleteFeed` (variant 114). Accounts: `[feed, globalstate]`.
pub fn delete_feed(
    program_id: &Pubkey,
    payer: &Pubkey,
    feed: &Pubkey,
    args: FeedDeleteArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::DeleteFeed(args),
        vec![
            AccountMeta::new(*feed, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `HaltFeed` (variant 120). Accounts: `[feed, globalstate]`.
///
/// Stops a feed publishing, reversibly. Unlike the other feed instructions, the feed's own
/// `builder` may sign this one, so a builder can rotate its upstream source without an operator.
/// A `FEED_AUTHORITY` or `FOUNDATION` key can sign it too, which is why this stays on the
/// permission-appending path.
pub fn halt_feed(program_id: &Pubkey, payer: &Pubkey, feed: &Pubkey) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::HaltFeed(FeedHaltArgs {}),
        vec![
            AccountMeta::new(*feed, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `ActivateFeed` (variant 124). Accounts: `[feed, globalstate]`, plus the bond mirror for a
/// bonded feed.
///
/// Admits a feed that was waiting on a conformance verdict. Signed by a `FEED_AUTHORITY` or
/// `FOUNDATION` key and by nobody else: the feed's own builder can halt and resume, but a builder
/// that could admit its own feed would be attesting to its own conformance.
///
/// `bond_mirror` is required for a bonded feed and must be `None` for a catalog one, as in
/// `resume_feed`. This is the step that re-reads the mirror, so a bonded feed without it is refused
/// rather than read as having no bond to check.
pub fn activate_feed(
    program_id: &Pubkey,
    payer: &Pubkey,
    feed: &Pubkey,
    bond_mirror: Option<&Pubkey>,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission_after_system(
        program_id,
        DoubleZeroInstruction::ActivateFeed(FeedActivateArgs {}),
        vec![
            AccountMeta::new(*feed, false),
            AccountMeta::new(globalstate, false),
        ],
        bond_mirror
            .map(|bond_mirror| AccountMeta::new_readonly(*bond_mirror, false))
            .into_iter()
            .collect(),
        payer,
    )
}

/// `ResumeFeed` (variant 121). Accounts: `[feed, globalstate]`, then the bond mirror.
///
/// Puts a halted feed back to publishing. Signed by the keys `halt_feed` accepts, except that an
/// operator's halt takes an operator to lift.
///
/// `bond_mirror` is required for a bonded feed and must be `None` for a catalog one. Resume
/// re-proves that the bond still covers the feed's rate, because a mirror can be corrected
/// downward while a feed sits halted, so a bonded feed without its mirror is refused rather than
/// read as having no bond to check. It rides after the payer and system program, found by its
/// address rather than its position, so a catalog feed's caller is not forced to send one.
pub fn resume_feed(
    program_id: &Pubkey,
    payer: &Pubkey,
    feed: &Pubkey,
    bond_mirror: Option<&Pubkey>,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission_after_system(
        program_id,
        DoubleZeroInstruction::ResumeFeed(FeedResumeArgs {}),
        vec![
            AccountMeta::new(*feed, false),
            AccountMeta::new(globalstate, false),
        ],
        bond_mirror
            .map(|bond_mirror| AccountMeta::new_readonly(*bond_mirror, false))
            .into_iter()
            .collect(),
        payer,
    )
}

/// `RetireFeed` (variant 122). Accounts: `[feed, globalstate]`.
///
/// Starts the notice seat holders are owed and stops new seats being sold. Signed by the keys
/// `halt_feed` accepts, the feed's own builder included, because a builder that wants out of
/// running a feed should not need an operator to stop.
///
/// This does not retire the feed. `finalize_feed_retirement` does, once the notice elapses.
pub fn retire_feed(program_id: &Pubkey, payer: &Pubkey, feed: &Pubkey) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::RetireFeed(FeedRetireArgs {}),
        vec![
            AccountMeta::new(*feed, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `FinalizeFeedRetirement` (variant 123). Accounts: `[feed]`.
///
/// Moves a feed from `Retiring` to `Retired` once its notice has elapsed. Permissionless, and so
/// on the no-permission path: the clock decides the outcome, and this instruction can only agree
/// with it. No globalstate either, because there is no authority to check against.
pub fn finalize_feed_retirement(program_id: &Pubkey, payer: &Pubkey, feed: &Pubkey) -> Instruction {
    common::build(
        program_id,
        DoubleZeroInstruction::FinalizeFeedRetirement(FeedFinalizeRetirementArgs {}),
        vec![AccountMeta::new(*feed, false)],
        payer,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_system_interface::program as system_program;

    #[test]
    fn test_a_builders_update_sends_only_the_groups_it_adds() {
        let pid = Pubkey::new_unique();
        let builder = Pubkey::new_unique();
        let feed = Pubkey::new_unique();
        let kept = Pubkey::new_unique();
        let added = Pubkey::new_unique();
        let args = FeedUpdateArgs {
            groups: Some(vec![kept, added]),
            ..Default::default()
        };

        let update = update_feed_as_builder(&pid, &builder, &feed, &[kept], args.clone());
        let n = update.accounts.len();
        assert_eq!(
            update.accounts[n - 2],
            AccountMeta::new_readonly(added, false)
        );
        assert!(!update.accounts.iter().any(|a| a.pubkey == kept));
        assert_eq!(
            update.accounts[n - 1],
            common::permission_meta(&pid, &builder)
        );

        // An admin's update sends no group accounts at all.
        let admin = update_feed(&pid, &builder, &feed, args);
        assert!(!admin
            .accounts
            .iter()
            .any(|a| a.pubkey == kept || a.pubkey == added));
    }

    /// A bonded feed carries its mirror; a catalog feed must not, or a caller that sends neither
    /// stops working.
    #[test]
    fn test_create_feed_appends_the_bond_mirror_only_when_bonded() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let exchange = Pubkey::new_unique();
        let builder = Pubkey::new_unique();
        let bond_ref = Pubkey::new_unique();

        let group = Pubkey::new_unique();
        let bonded_args = FeedCreateArgs {
            code: "bonded".to_string(),
            name: "Bonded".to_string(),
            exchange,
            groups: vec![group],
            builder,
            bond_ref,
            spec_id: "top-of-book@v1.0.0".to_string(),
            sla_hash: [9u8; 32],
            committed_rate_bits_per_sec: 1_000_000_000,
            ..Default::default()
        };
        let bonded = create_feed(&pid, &builder, bonded_args.clone());
        let (mirror, _) = get_bond_mirror_pda(&pid, &bond_ref);
        assert_eq!(
            bonded.accounts[bonded.accounts.len() - 3],
            AccountMeta::new(mirror, false),
            "the mirror is writable: creating the feed claims the bond"
        );
        assert_eq!(
            bonded.accounts[bonded.accounts.len() - 2],
            AccountMeta::new_readonly(group, false),
            "each group rides read-only, so the program can read its owner"
        );
        assert_eq!(
            bonded.accounts.last().unwrap(),
            &common::permission_meta(&pid, &builder),
            "the Permission account stays last, after the mirror and the groups"
        );

        // An admin creating a bonded feed sends the mirror but no groups: the program does not
        // read them, and at the group limit they would not fit.
        let by_admin = create_feed(&pid, &payer, bonded_args);
        assert_eq!(
            by_admin.accounts[by_admin.accounts.len() - 2],
            AccountMeta::new(mirror, false)
        );
        assert!(!by_admin.accounts.iter().any(|a| a.pubkey == group));

        let catalog = create_feed(
            &pid,
            &payer,
            FeedCreateArgs {
                code: "catalog".to_string(),
                name: "Catalog".to_string(),
                exchange,
                groups: vec![Pubkey::new_unique()],
                ..Default::default()
            },
        );
        assert!(
            !catalog
                .accounts
                .iter()
                .any(|a| a.pubkey == get_bond_mirror_pda(&pid, &Pubkey::default()).0),
            "a catalog feed sends no mirror"
        );
    }

    #[test]
    fn test_create_feed_derives_pda_from_code_and_exchange() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let exchange = Pubkey::new_unique();
        let args = FeedCreateArgs {
            code: "feed".to_string(),
            name: "Feed".to_string(),
            exchange,
            groups: vec![Pubkey::new_unique()],
            ..Default::default()
        };
        let ix = create_feed(&pid, &payer, args);
        assert_eq!(ix.data[0], 112);
        let (feed, _) = get_feed_pda(&pid, "feed", &exchange);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(feed, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
                common::permission_meta(&pid, &payer),
            ]
        );
    }

    #[test]
    fn test_feed_pubkey_verbs() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let feed = Pubkey::new_unique();
        let (globalstate, _) = get_globalstate_pda(&pid);
        let expected = vec![
            AccountMeta::new(feed, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(payer, true),
            AccountMeta::new(system_program::ID, false),
            common::permission_meta(&pid, &payer),
        ];
        // Realistic args: an all-`None` (default) update is rejected by the
        // processor as a no-op (`InvalidArgument`), so the fixture would not execute.
        let update = update_feed(
            &pid,
            &payer,
            &feed,
            FeedUpdateArgs {
                name: Some("Feed".to_string()),
                groups: None,
                feed_chain: None,
            },
        );
        assert_eq!(update.data[0], 113);
        assert_eq!(update.accounts, expected);
        let delete = delete_feed(&pid, &payer, &feed, FeedDeleteArgs {});
        assert_eq!(delete.data[0], 114);
        assert_eq!(delete.accounts, expected);

        // The lifecycle verbs take the same accounts. `unpack` matches the leading byte by hand
        // with a catch-all, so a wrong tag here reaches the program as `InvalidInstructionData`
        // rather than as a compile error.
        let halt = halt_feed(&pid, &payer, &feed);
        assert_eq!(halt.data[0], 120);
        assert_eq!(halt.accounts, expected);
        let resume = resume_feed(&pid, &payer, &feed, None);
        assert_eq!(resume.data[0], 121);
        assert_eq!(resume.accounts, expected);

        let activate = activate_feed(&pid, &payer, &feed, None);
        assert_eq!(activate.data[0], 124);
        assert_eq!(activate.accounts, expected);

        // A bonded feed's mirror rides after the payer and system program, read-only, because the
        // step reads it rather than claiming it. `resume_feed` places it the same way, and the
        // processor finds it by address rather than position, so its place in the list is the
        // caller-side half of that contract.
        let mirror = Pubkey::new_unique();
        // The Permission account stays last, after the mirror.
        let (permission, before_permission) = expected.split_last().unwrap();
        let bonded_expected = [
            before_permission.to_vec(),
            vec![AccountMeta::new_readonly(mirror, false), permission.clone()],
        ]
        .concat();
        let bonded_activate = activate_feed(&pid, &payer, &feed, Some(&mirror));
        assert_eq!(bonded_activate.data[0], 124);
        assert_eq!(bonded_activate.accounts, bonded_expected);
        let bonded_resume = resume_feed(&pid, &payer, &feed, Some(&mirror));
        assert_eq!(bonded_resume.accounts, bonded_expected);

        let retire = retire_feed(&pid, &payer, &feed);
        assert_eq!(retire.data[0], 122);
        assert_eq!(retire.accounts, expected);

        // Finalize is permissionless, so it carries no globalstate: nothing about it is checked
        // against an authority, and sending one would ask a caller for an account the processor
        // never reads.
        let finalize = finalize_feed_retirement(&pid, &payer, &feed);
        assert_eq!(finalize.data[0], 123);
        assert_eq!(
            finalize.accounts,
            vec![
                AccountMeta::new(feed, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );

        // A bonded feed's mirror rides after the payer and system program, where the processor
        // looks for it. Without it, resume refuses with `BondMirrorMissing`.
        let mirror = Pubkey::new_unique();
        let bonded = resume_feed(&pid, &payer, &feed, Some(&mirror));
        assert_eq!(
            bonded.accounts[..expected.len() - 1],
            expected[..expected.len() - 1]
        );
        assert_eq!(
            bonded.accounts[bonded.accounts.len() - 2..],
            [
                AccountMeta::new_readonly(mirror, false),
                common::permission_meta(&pid, &payer),
            ]
        );
    }
}
