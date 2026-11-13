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
//! appends the payer's Permission PDA last. The group accounts of create, update
//! and delete come right after the system program, and a bonded feed's bond
//! mirror follows them.

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_bond_mirror_pda, get_feed_pda, get_globalstate_pda},
    processors::feed::{
        activate::FeedActivateArgs, create::FeedCreateArgs, delete::FeedDeleteArgs,
        finalize_retirement::FeedFinalizeRetirementArgs, halt::FeedHaltArgs,
        resume::FeedResumeArgs, retire::FeedRetireArgs, update::FeedUpdateArgs,
    },
    state::feed::Feed,
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// `CreateFeed` (variant 112). Accounts: `[feed, globalstate]`, then each of `groups`.
///
/// The feed PDA is derived from `args.code` and `args.exchange`. The program reads the feed's
/// groups from their accounts and adds one to the feed count of each, so `args.group_count` is
/// set here from `groups`.
pub fn create_feed(
    program_id: &Pubkey,
    payer: &Pubkey,
    groups: &[Pubkey],
    mut args: FeedCreateArgs,
) -> Instruction {
    let (feed, _) = get_feed_pda(program_id, &args.code, &args.exchange);
    let (globalstate, _) = get_globalstate_pda(program_id);
    args.group_count = group_count(groups);
    let bond_mirror_accounts = bond_mirror_accounts(program_id, &args);
    common::build_with_permission_after_system(
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        vec![
            AccountMeta::new(feed, false),
            AccountMeta::new(globalstate, false),
        ],
        [group_accounts(groups), bond_mirror_accounts].concat(),
        payer,
    )
}

/// `UpdateFeed` (variant 113). Accounts: `[feed, globalstate]`, then each of `added`, then each
/// of `removed`. The program changes the feed count of each group, so `args` gets both counts
/// here. `read_groups` is the feed's group list as the caller read it. A group change fails when
/// the feed's groups changed since then. A bonded feed's builder can add groups but not remove
/// them.
pub fn update_feed(
    program_id: &Pubkey,
    payer: &Pubkey,
    feed: &Pubkey,
    read_groups: &[Pubkey],
    added: &[Pubkey],
    removed: &[Pubkey],
    mut args: FeedUpdateArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    args.added_group_count = group_count(added);
    args.removed_group_count = group_count(removed);
    args.expected_groups_hash = Feed::groups_hash(read_groups);
    let group_accounts = [group_accounts(added), group_accounts(removed)].concat();
    common::build_with_permission_after_system(
        program_id,
        DoubleZeroInstruction::UpdateFeed(args),
        vec![
            AccountMeta::new(*feed, false),
            AccountMeta::new(globalstate, false),
        ],
        group_accounts,
        payer,
    )
}

/// `DeleteFeed` (variant 114). Accounts: `[feed, globalstate]`, then each group the feed lists,
/// in the feed's order.
pub fn delete_feed(
    program_id: &Pubkey,
    payer: &Pubkey,
    feed: &Pubkey,
    groups: &[Pubkey],
    args: FeedDeleteArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission_after_system(
        program_id,
        DoubleZeroInstruction::DeleteFeed(args),
        vec![
            AccountMeta::new(*feed, false),
            AccountMeta::new(globalstate, false),
        ],
        group_accounts(groups),
        payer,
    )
}

// Writable: the program changes the feed count of each group.
fn group_accounts(groups: &[Pubkey]) -> Vec<AccountMeta> {
    groups
        .iter()
        .map(|group| AccountMeta::new(*group, false))
        .collect()
}

// A bonded feed needs its bond mirror: `CreateFeed` reads the tier from it and writes the feed's
// key onto it to claim the bond. Derived rather than taken as a parameter, because unlike
// `resume_feed` the args already carry the bond it is seeded on, so asking a caller for it would
// only create a way to get it wrong. Writable: creating the feed claims it.
fn bond_mirror_accounts(program_id: &Pubkey, args: &FeedCreateArgs) -> Vec<AccountMeta> {
    if args.builder == Pubkey::default() {
        return Vec::new();
    }
    vec![AccountMeta::new(
        get_bond_mirror_pda(program_id, &args.bond_ref).0,
        false,
    )]
}

// A count past 255 is past the 20-group limit, and the program refuses it.
fn group_count(groups: &[Pubkey]) -> u8 {
    u8::try_from(groups.len()).unwrap_or(u8::MAX)
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
    fn test_update_feed_sends_the_added_then_the_removed_groups() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let feed = Pubkey::new_unique();
        let added = Pubkey::new_unique();
        let removed = Pubkey::new_unique();
        let (globalstate, _) = get_globalstate_pda(&pid);

        let update = update_feed(
            &pid,
            &payer,
            &feed,
            &[removed],
            &[added],
            &[removed],
            FeedUpdateArgs::default(),
        );

        assert_eq!(
            update.accounts,
            vec![
                AccountMeta::new(feed, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
                AccountMeta::new(added, false),
                AccountMeta::new(removed, false),
                common::permission_meta(&pid, &payer),
            ]
        );
        assert_eq!(
            DoubleZeroInstruction::unpack(&update.data).unwrap(),
            DoubleZeroInstruction::UpdateFeed(FeedUpdateArgs {
                added_group_count: 1,
                removed_group_count: 1,
                expected_groups_hash: Feed::groups_hash(&[removed]),
                ..Default::default()
            })
        );
    }

    /// The groups come right after the system program, so the bond mirror and the Permission
    /// account keep their places at the end.
    #[test]
    fn test_create_feed_sends_the_groups_before_the_bond_mirror() {
        let pid = Pubkey::new_unique();
        let builder = Pubkey::new_unique();
        let exchange = Pubkey::new_unique();
        let bond_ref = Pubkey::new_unique();
        let group = Pubkey::new_unique();
        let (feed, _) = get_feed_pda(&pid, "bonded", &exchange);
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (mirror, _) = get_bond_mirror_pda(&pid, &bond_ref);

        let bonded = create_feed(
            &pid,
            &builder,
            &[group],
            FeedCreateArgs {
                code: "bonded".to_string(),
                name: "Bonded".to_string(),
                exchange,
                builder,
                bond_ref,
                ..Default::default()
            },
        );

        assert_eq!(
            bonded.accounts,
            vec![
                AccountMeta::new(feed, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(builder, true),
                AccountMeta::new(system_program::ID, false),
                AccountMeta::new(group, false),
                AccountMeta::new(mirror, false),
                common::permission_meta(&pid, &builder),
            ]
        );
    }

    #[test]
    fn test_create_feed_derives_pda_from_code_and_exchange() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let exchange = Pubkey::new_unique();
        let group = Pubkey::new_unique();
        let args = FeedCreateArgs {
            code: "feed".to_string(),
            name: "Feed".to_string(),
            exchange,
            ..Default::default()
        };
        let ix = create_feed(&pid, &payer, &[group], args.clone());
        assert_eq!(
            DoubleZeroInstruction::unpack(&ix.data).unwrap(),
            DoubleZeroInstruction::CreateFeed(FeedCreateArgs {
                group_count: 1,
                ..args
            })
        );
        let (feed, _) = get_feed_pda(&pid, "feed", &exchange);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(feed, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
                AccountMeta::new(group, false),
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
            &[],
            &[],
            &[],
            FeedUpdateArgs {
                name: Some("Feed".to_string()),
                ..Default::default()
            },
        );
        assert_eq!(update.data[0], 113);
        assert_eq!(update.accounts, expected);
        let delete = delete_feed(&pid, &payer, &feed, &[], FeedDeleteArgs {});
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
