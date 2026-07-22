//! Feed-domain instruction builders.
//!
//! All route through `authorize()` -> [`common::build_with_permission`].
//! Accounts are `[feed, globalstate]`; create derives the feed PDA from
//! `args.code` and `args.exchange`.

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_feed_pda, get_globalstate_pda},
    processors::feed::{create::FeedCreateArgs, delete::FeedDeleteArgs, update::FeedUpdateArgs},
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
        let update = update_feed(&pid, &payer, &feed, FeedUpdateArgs::default());
        assert_eq!(update.data[0], 113);
        assert_eq!(update.accounts, expected);
        let delete = delete_feed(&pid, &payer, &feed, FeedDeleteArgs {});
        assert_eq!(delete.data[0], 114);
        assert_eq!(delete.accounts, expected);
    }
}
