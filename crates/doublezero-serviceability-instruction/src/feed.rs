//! Feed-domain instruction builders.
//!
//! All route through `authorize()` -> [`common::build_with_permission`].
//! Accounts are `[feed, globalstate]`; create derives the feed PDA from
//! `args.code` and `args.exchange`.
//!
//! **Why `build_with_permission`.** The feed `create`/`update`/`delete`
//! processors call `authorize(.., FEED_AUTHORITY | FOUNDATION)`, so feed is a
//! permission-gated instruction like topology/tenant and belongs on the
//! permission-appending path. The Rust SDK feed commands now also append the
//! Permission account after an RPC existence check
//! (`append_payer_permission_account`). These builders stay on the deferred
//! [`common::build_with_permission`] path: they do not append the account
//! themselves. The SDK activates the append caller-side for feed
//! create/update/delete, user delete/update, and multicast role updates.
//!
//! One pre-existing program-side gap, unrelated to these builders: `FEED_AUTHORITY`
//! is absent from `authorize.rs::AUTHORIZE_GATED_FLAGS`. That list drives
//! `doublezero permission audit` and the `RequirePermissionAccounts` rollout, so
//! feed must be added there before the deferred permission append is activated
//! (see [`common::build_with_permission`]) — otherwise feed-authority keys would
//! not be guaranteed a Permission account at activation. Closing that gap is a
//! serviceability-program change, out of scope for this crate; it is tracked in
//! <https://github.com/malbeclabs/doublezero/issues/4079>. The
//! `feed_authority_gap_is_still_open` canary test below asserts the gap currently
//! exists, so closing it forces this note to be revisited.

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_feed_pda, get_globalstate_pda},
    processors::feed::{
        create::FeedCreateArgs, delete::FeedDeleteArgs, halt::FeedHaltArgs, resume::FeedResumeArgs,
        update::FeedUpdateArgs,
    },
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// `CreateFeed` (variant 112). Accounts: `[feed, globalstate]`.
///
/// The feed PDA is derived from `args.code` and `args.exchange`.
pub fn create_feed(program_id: &Pubkey, payer: &Pubkey, args: FeedCreateArgs) -> Instruction {
    let (feed, _) = get_feed_pda(program_id, &args.code, &args.exchange);
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CreateFeed(args),
        vec![
            AccountMeta::new(feed, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `UpdateFeed` (variant 113). Accounts: `[feed, globalstate]`.
pub fn update_feed(
    program_id: &Pubkey,
    payer: &Pubkey,
    feed: &Pubkey,
    args: FeedUpdateArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::UpdateFeed(args),
        vec![
            AccountMeta::new(*feed, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
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

/// `ResumeFeed` (variant 121). Accounts: `[feed, globalstate]`, then the stake mirror.
///
/// Puts a halted feed back to publishing. Signed by the keys `halt_feed` accepts, except that an
/// operator's halt takes an operator to lift.
///
/// `stake_mirror` is required for a staked feed and must be `None` for a catalog one. Resume
/// re-proves that the stake still covers the feed's rate, because a mirror can be corrected
/// downward while a feed sits halted, so a staked feed without its mirror is refused rather than
/// read as having no stake to check. It rides after the payer and system program, found by its
/// address rather than its position, so a catalog feed's caller is not forced to send one.
pub fn resume_feed(
    program_id: &Pubkey,
    payer: &Pubkey,
    feed: &Pubkey,
    stake_mirror: Option<&Pubkey>,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    let mut ix = common::build_with_permission(
        program_id,
        DoubleZeroInstruction::ResumeFeed(FeedResumeArgs {}),
        vec![
            AccountMeta::new(*feed, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    );
    if let Some(stake_mirror) = stake_mirror {
        ix.accounts
            .push(AccountMeta::new_readonly(*stake_mirror, false));
    }
    ix
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_system_interface::program as system_program;

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

        // A staked feed's mirror rides after the payer and system program, where the processor
        // looks for it. Without it, resume refuses with `StakeMirrorMissing`.
        let mirror = Pubkey::new_unique();
        let staked = resume_feed(&pid, &payer, &feed, Some(&mirror));
        assert_eq!(staked.accounts[..expected.len()], expected[..]);
        assert_eq!(
            staked.accounts.last().unwrap(),
            &AccountMeta::new_readonly(mirror, false)
        );
    }

    /// Tripwire for the module-doc note: `FEED_AUTHORITY` is currently absent from
    /// `AUTHORIZE_GATED_FLAGS` even though the feed processors gate on it. When the
    /// program-side gap (https://github.com/malbeclabs/doublezero/issues/4079) is
    /// closed, this test fails — a signal that the deferred permission append is
    /// now safe to activate for feed and that this crate's doc note must be updated.
    #[test]
    fn feed_authority_gap_is_still_open() {
        use doublezero_serviceability::{
            authorize::AUTHORIZE_GATED_FLAGS, state::permission::permission_flags,
        };
        assert!(
            !AUTHORIZE_GATED_FLAGS.contains(&permission_flags::FEED_AUTHORITY),
            "FEED_AUTHORITY is now gated: close issue #4079 tracking, then update the \
             feed.rs module doc and remove this canary"
        );
    }
}
