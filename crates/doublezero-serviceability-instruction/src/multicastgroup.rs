//! Multicast-group-domain instruction builders (incl. pub/sub allowlists).
//!
//! All route through `authorize()` -> [`common::build_with_permission`].

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalstate_pda, get_multicastgroup_pda, get_resource_extension_pda},
    processors::multicastgroup::{
        allowlist::{
            publisher::{
                add::AddMulticastGroupPubAllowlistArgs,
                remove::RemoveMulticastGroupPubAllowlistArgs,
            },
            subscriber::{
                add::AddMulticastGroupSubAllowlistArgs,
                remove::RemoveMulticastGroupSubAllowlistArgs,
            },
        },
        create::MulticastGroupCreateArgs,
        delete::MulticastGroupDeleteArgs,
        reactivate::MulticastGroupReactivateArgs,
        subscribe::UpdateMulticastGroupRolesArgs,
        subscribe_feed::SubscribeFeedArgs,
        suspend::MulticastGroupSuspendArgs,
        unsubscribe_feed::UnsubscribeFeedArgs,
        update::MulticastGroupUpdateArgs,
    },
    resource::ResourceType,
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// `CreateMulticastGroup` (variant 46).
/// Accounts: `[mgroup, globalstate, multicast_group_block]`.
///
/// `account_index` is the new group's index (`globalstate.account_index + 1`).
pub fn create_multicast_group(
    program_id: &Pubkey,
    payer: &Pubkey,
    account_index: u128,
    mut args: MulticastGroupCreateArgs,
) -> Instruction {
    let (mgroup, _) = get_multicastgroup_pda(program_id, account_index);
    let (globalstate, _) = get_globalstate_pda(program_id);
    let (multicast_group_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::MulticastGroupBlock);
    // The processor rejects `use_onchain_allocation == false` as its first
    // statement (multicastgroup/create.rs), and `false` is the struct default —
    // a caller-supplied value here can only ever fail. This builder always emits
    // the `multicast_group_block` account, so it forces the flag (as the SDK does).
    args.use_onchain_allocation = true;
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(args),
        vec![
            AccountMeta::new(mgroup, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(multicast_group_block, false),
        ],
        payer,
    )
}

/// `UpdateMulticastGroup` (variant 49).
/// Accounts: `[mgroup, globalstate]`, plus `multicast_group_block` when
/// `args.multicast_ip.is_some()` (a multicast-IP reallocation).
///
/// `args.use_onchain_allocation` is DERIVED from `args.multicast_ip.is_some()`;
/// any caller-supplied value is ignored (the flag must stay in lockstep with
/// whether the block account is emitted).
pub fn update_multicast_group(
    program_id: &Pubkey,
    payer: &Pubkey,
    mgroup: &Pubkey,
    mut args: MulticastGroupUpdateArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    let mut accounts = vec![
        AccountMeta::new(*mgroup, false),
        AccountMeta::new(globalstate, false),
    ];
    let updating_multicast_ip = args.multicast_ip.is_some();
    if updating_multicast_ip {
        let (multicast_group_block, _, _) =
            get_resource_extension_pda(program_id, ResourceType::MulticastGroupBlock);
        accounts.push(AccountMeta::new(multicast_group_block, false));
    }
    // Derived, not caller-supplied: keep the flag in lockstep with block-account
    // emission. A stray `true` with no block emitted would make the processor
    // consume the trailing payer as the resource-extension account.
    args.use_onchain_allocation = updating_multicast_ip;
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::UpdateMulticastGroup(args),
        accounts,
        payer,
    )
}

/// `SuspendMulticastGroup` (variant 50). Accounts: `[mgroup, globalstate]`.
pub fn suspend_multicast_group(
    program_id: &Pubkey,
    payer: &Pubkey,
    mgroup: &Pubkey,
    args: MulticastGroupSuspendArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::SuspendMulticastGroup(args),
        vec![
            AccountMeta::new(*mgroup, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `ReactivateMulticastGroup` (variant 51). Accounts: `[mgroup, globalstate]`.
pub fn reactivate_multicast_group(
    program_id: &Pubkey,
    payer: &Pubkey,
    mgroup: &Pubkey,
    args: MulticastGroupReactivateArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::ReactivateMulticastGroup(args),
        vec![
            AccountMeta::new(*mgroup, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `DeleteMulticastGroup` (variant 52).
/// Accounts: `[mgroup, globalstate, multicast_group_block, owner]`.
pub fn delete_multicast_group(
    program_id: &Pubkey,
    payer: &Pubkey,
    mgroup: &Pubkey,
    owner: &Pubkey,
    mut args: MulticastGroupDeleteArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    let (multicast_group_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::MulticastGroupBlock);
    // The processor rejects `use_onchain_deallocation == false` as its first
    // statement (multicastgroup/delete.rs), and `false` is the struct default —
    // a caller-supplied value here can only ever fail. This builder always emits
    // the `multicast_group_block` account, so it forces the flag (as the SDK does).
    args.use_onchain_deallocation = true;
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::DeleteMulticastGroup(args),
        vec![
            AccountMeta::new(*mgroup, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(multicast_group_block, false),
            AccountMeta::new(*owner, false),
        ],
        payer,
    )
}

/// `UpdateMulticastGroupRoles` (variant 58) — publisher/subscriber role change.
/// Accounts: `[group, accesspass, user, globalstate, multicast_publisher_block]`.
pub fn update_multicast_group_roles(
    program_id: &Pubkey,
    payer: &Pubkey,
    group: &Pubkey,
    accesspass: &Pubkey,
    user: &Pubkey,
    mut args: UpdateMulticastGroupRolesArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    let (multicast_publisher_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::MulticastPublisherBlock);
    // The processor rejects `use_onchain_allocation == false` as its first
    // statement (multicastgroup/subscribe.rs), and `false` is the struct default —
    // a caller-supplied value here can only ever fail. This builder always emits
    // the `multicast_publisher_block` account, so it forces the flag (as the SDK does).
    args.use_onchain_allocation = true;
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::UpdateMulticastGroupRoles(args),
        vec![
            AccountMeta::new(*group, false),
            AccountMeta::new(*accesspass, false),
            AccountMeta::new(*user, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(multicast_publisher_block, false),
        ],
        payer,
    )
}

/// `SubscribeFeed` (variant 117) — join whole feeds on an EdgeSeat access pass.
///
/// Accounts: `[accesspass, user, globalstate, device, feeds.., groups..]`.
///
/// `groups` must be exactly the groups this call adds; the processor derives that set from the feeds
/// and rejects a mismatch, so a stale client cannot half-apply a change. `feed_count` is derived from
/// `feeds.len()` rather than trusted from the caller.
pub fn subscribe_feed(
    program_id: &Pubkey,
    payer: &Pubkey,
    accesspass: &Pubkey,
    user: &Pubkey,
    device: &Pubkey,
    feeds: &[Pubkey],
    groups: &[Pubkey],
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    let mut accounts = vec![
        AccountMeta::new(*accesspass, false),
        AccountMeta::new(*user, false),
        AccountMeta::new(globalstate, false),
        AccountMeta::new_readonly(*device, false),
    ];
    accounts.extend(
        feeds
            .iter()
            .map(|feed| AccountMeta::new_readonly(*feed, false)),
    );
    accounts.extend(groups.iter().map(|group| AccountMeta::new(*group, false)));

    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::SubscribeFeed(SubscribeFeedArgs {
            feed_count: feeds.len() as u8,
        }),
        accounts,
        payer,
    )
}

/// `UnsubscribeFeed` (variant 118) — leave whole feeds on an EdgeSeat access pass.
///
/// Accounts: `[accesspass, user, globalstate, device, targets.., retained.., groups..]`.
///
/// `retained` must be every feed the user keeps: two feeds on one pass can carry the same group, and
/// without the retained group sets the processor would drop a group another held feed still covers and
/// strand that feed's seat. Together `targets` and `retained` must cover every held feed still
/// provisioned on the pass; a held feed the pass dropped is pruned by the processor instead.
#[allow(clippy::too_many_arguments)]
pub fn unsubscribe_feed(
    program_id: &Pubkey,
    payer: &Pubkey,
    accesspass: &Pubkey,
    user: &Pubkey,
    device: &Pubkey,
    targets: &[Pubkey],
    retained: &[Pubkey],
    groups: &[Pubkey],
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    let mut accounts = vec![
        AccountMeta::new(*accesspass, false),
        AccountMeta::new(*user, false),
        AccountMeta::new(globalstate, false),
        AccountMeta::new_readonly(*device, false),
    ];
    accounts.extend(
        targets
            .iter()
            .chain(retained)
            .map(|feed| AccountMeta::new_readonly(*feed, false)),
    );
    accounts.extend(groups.iter().map(|group| AccountMeta::new(*group, false)));

    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::UnsubscribeFeed(UnsubscribeFeedArgs {
            feed_count: targets.len() as u8,
            retained_feed_count: retained.len() as u8,
        }),
        accounts,
        payer,
    )
}

/// `AddMulticastGroupPubAllowlist` (variant 54).
/// Accounts: `[mgroup, accesspass, globalstate, user_payer]`.
pub fn add_multicast_group_pub_allowlist(
    program_id: &Pubkey,
    payer: &Pubkey,
    mgroup: &Pubkey,
    accesspass: &Pubkey,
    args: AddMulticastGroupPubAllowlistArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    // Single source of truth: the processor derives both the accesspass PDA and
    // the funded key from `args.user_payer`, so the account meta MUST come from
    // the same field — never a separate parameter that could diverge.
    let user_payer = args.user_payer;
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(args),
        vec![
            AccountMeta::new(*mgroup, false),
            AccountMeta::new(*accesspass, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(user_payer, false),
        ],
        payer,
    )
}

/// `RemoveMulticastGroupPubAllowlist` (variant 55).
/// Accounts: `[mgroup, accesspass, globalstate]`.
pub fn remove_multicast_group_pub_allowlist(
    program_id: &Pubkey,
    payer: &Pubkey,
    mgroup: &Pubkey,
    accesspass: &Pubkey,
    args: RemoveMulticastGroupPubAllowlistArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::RemoveMulticastGroupPubAllowlist(args),
        vec![
            AccountMeta::new(*mgroup, false),
            AccountMeta::new(*accesspass, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `AddMulticastGroupSubAllowlist` (variant 56).
/// Accounts: `[mgroup, accesspass, globalstate, user_payer]`.
pub fn add_multicast_group_sub_allowlist(
    program_id: &Pubkey,
    payer: &Pubkey,
    mgroup: &Pubkey,
    accesspass: &Pubkey,
    args: AddMulticastGroupSubAllowlistArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    // Single source of truth: the processor derives both the accesspass PDA and
    // the funded key from `args.user_payer`, so the account meta MUST come from
    // the same field — never a separate parameter that could diverge.
    let user_payer = args.user_payer;
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::AddMulticastGroupSubAllowlist(args),
        vec![
            AccountMeta::new(*mgroup, false),
            AccountMeta::new(*accesspass, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(user_payer, false),
        ],
        payer,
    )
}

/// `RemoveMulticastGroupSubAllowlist` (variant 57).
/// Accounts: `[mgroup, accesspass, globalstate]`.
pub fn remove_multicast_group_sub_allowlist(
    program_id: &Pubkey,
    payer: &Pubkey,
    mgroup: &Pubkey,
    accesspass: &Pubkey,
    args: RemoveMulticastGroupSubAllowlistArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::RemoveMulticastGroupSubAllowlist(args),
        vec![
            AccountMeta::new(*mgroup, false),
            AccountMeta::new(*accesspass, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_system_interface::program as system_program;
    use std::net::Ipv4Addr;

    /// The one feed transaction that cannot be split: a single-target leave must name every held
    /// feed, so its worst case is `MAX_USER_FEEDS - 1` retained plus `MAX_FEED_GROUPS` departing
    /// groups. Pin that it fits a 1232-byte legacy transaction behind the compute-budget prelude
    /// `send_transaction` prepends.
    #[test]
    fn test_worst_case_leave_fits_one_transaction() {
        use doublezero_serviceability::processors::{
            feed::create::MAX_FEED_GROUPS, multicastgroup::subscribe_feed::MAX_USER_FEEDS,
        };

        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let targets = [Pubkey::new_unique()];
        let retained: Vec<Pubkey> = (0..MAX_USER_FEEDS - 1)
            .map(|_| Pubkey::new_unique())
            .collect();
        let groups: Vec<Pubkey> = (0..MAX_FEED_GROUPS).map(|_| Pubkey::new_unique()).collect();
        let ix = unsubscribe_feed(
            &pid,
            &payer,
            &Pubkey::new_unique(),
            &Pubkey::new_unique(),
            &Pubkey::new_unique(),
            &targets,
            &retained,
            &groups,
        );
        let mut instructions = common::compute_budget_prelude().to_vec();
        instructions.push(ix);
        let message = solana_sdk::message::Message::new(&instructions, Some(&payer));
        // One byte of signature count plus one 64-byte signature plus the message.
        let tx_size = 1 + 64 + message.serialize().len();
        assert!(tx_size <= 1232, "worst-case leave is {tx_size} bytes");
    }

    #[test]
    fn test_create_multicast_group() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let ix = create_multicast_group(&pid, &payer, 1, MulticastGroupCreateArgs::default());
        assert_eq!(ix.data[0], 46);
        let (mgroup, _) = get_multicastgroup_pda(&pid, 1);
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (block, _, _) = get_resource_extension_pda(&pid, ResourceType::MulticastGroupBlock);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(mgroup, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(block, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
        // The builder forces the flag on even though `::default()` leaves it off.
        match DoubleZeroInstruction::unpack(&ix.data).unwrap() {
            DoubleZeroInstruction::CreateMulticastGroup(a) => assert!(a.use_onchain_allocation),
            other => panic!("unexpected: {other:?}"),
        }
    }

    #[test]
    fn test_update_multicast_group_ip_change_adds_block() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let mgroup = Pubkey::new_unique();
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (block, _, _) = get_resource_extension_pda(&pid, ResourceType::MulticastGroupBlock);

        let with_ip = update_multicast_group(
            &pid,
            &payer,
            &mgroup,
            MulticastGroupUpdateArgs {
                multicast_ip: Some(Ipv4Addr::new(239, 1, 1, 1)),
                ..Default::default()
            },
        );
        assert_eq!(with_ip.data[0], 49);
        assert_eq!(
            with_ip.accounts,
            vec![
                AccountMeta::new(mgroup, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(block, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
        match DoubleZeroInstruction::unpack(&with_ip.data).unwrap() {
            DoubleZeroInstruction::UpdateMulticastGroup(a) => assert!(a.use_onchain_allocation),
            other => panic!("unexpected: {other:?}"),
        }

        // No multicast_ip -> no block, no onchain allocation.
        let without_ip =
            update_multicast_group(&pid, &payer, &mgroup, MulticastGroupUpdateArgs::default());
        assert_eq!(
            without_ip.accounts,
            vec![
                AccountMeta::new(mgroup, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
        // No block emitted -> the flag MUST stay off, or the processor would
        // consume the trailing payer as the resource-extension account.
        match DoubleZeroInstruction::unpack(&without_ip.data).unwrap() {
            DoubleZeroInstruction::UpdateMulticastGroup(a) => assert!(!a.use_onchain_allocation),
            other => panic!("unexpected: {other:?}"),
        }
    }

    #[test]
    fn test_mgroup_lifecycle_verbs() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let mgroup = Pubkey::new_unique();
        let (globalstate, _) = get_globalstate_pda(&pid);
        let expected = vec![
            AccountMeta::new(mgroup, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(payer, true),
            AccountMeta::new(system_program::ID, false),
        ];
        for (ix, tag) in [
            (
                suspend_multicast_group(&pid, &payer, &mgroup, MulticastGroupSuspendArgs {}),
                50,
            ),
            (
                reactivate_multicast_group(&pid, &payer, &mgroup, MulticastGroupReactivateArgs {}),
                51,
            ),
        ] {
            assert_eq!(ix.data[0], tag);
            assert_eq!(ix.accounts, expected);
        }
    }

    #[test]
    fn test_delete_multicast_group() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let mgroup = Pubkey::new_unique();
        let owner = Pubkey::new_unique();
        let ix = delete_multicast_group(
            &pid,
            &payer,
            &mgroup,
            &owner,
            MulticastGroupDeleteArgs::default(),
        );
        assert_eq!(ix.data[0], 52);
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (block, _, _) = get_resource_extension_pda(&pid, ResourceType::MulticastGroupBlock);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(mgroup, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(block, false),
                AccountMeta::new(owner, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
        // The builder forces the flag on even though `::default()` leaves it off.
        match DoubleZeroInstruction::unpack(&ix.data).unwrap() {
            DoubleZeroInstruction::DeleteMulticastGroup(a) => assert!(a.use_onchain_deallocation),
            other => panic!("unexpected: {other:?}"),
        }
    }

    #[test]
    fn test_update_multicast_group_roles() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let group = Pubkey::new_unique();
        let accesspass = Pubkey::new_unique();
        let user = Pubkey::new_unique();
        let args = UpdateMulticastGroupRolesArgs {
            client_ip: Ipv4Addr::new(192, 168, 1, 1),
            publisher: true,
            subscriber: false,
            // Left off deliberately: the builder must force it on.
            use_onchain_allocation: false,
        };
        let ix = update_multicast_group_roles(&pid, &payer, &group, &accesspass, &user, args);
        assert_eq!(ix.data[0], 58);
        match DoubleZeroInstruction::unpack(&ix.data).unwrap() {
            DoubleZeroInstruction::UpdateMulticastGroupRoles(a) => {
                assert!(a.use_onchain_allocation)
            }
            other => panic!("unexpected: {other:?}"),
        }
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (mpb, _, _) = get_resource_extension_pda(&pid, ResourceType::MulticastPublisherBlock);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(group, false),
                AccountMeta::new(accesspass, false),
                AccountMeta::new(user, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(mpb, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_allowlist_add_and_remove() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let mgroup = Pubkey::new_unique();
        let accesspass = Pubkey::new_unique();
        let user_payer = Pubkey::new_unique();
        let (globalstate, _) = get_globalstate_pda(&pid);
        let client_ip = Ipv4Addr::new(192, 168, 1, 1);

        let add_expected = vec![
            AccountMeta::new(mgroup, false),
            AccountMeta::new(accesspass, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(user_payer, false),
            AccountMeta::new(payer, true),
            AccountMeta::new(system_program::ID, false),
        ];
        let remove_expected = vec![
            AccountMeta::new(mgroup, false),
            AccountMeta::new(accesspass, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(payer, true),
            AccountMeta::new(system_program::ID, false),
        ];

        let add_pub = add_multicast_group_pub_allowlist(
            &pid,
            &payer,
            &mgroup,
            &accesspass,
            AddMulticastGroupPubAllowlistArgs {
                client_ip,
                user_payer,
            },
        );
        assert_eq!(add_pub.data[0], 54);
        assert_eq!(add_pub.accounts, add_expected);

        let remove_pub = remove_multicast_group_pub_allowlist(
            &pid,
            &payer,
            &mgroup,
            &accesspass,
            RemoveMulticastGroupPubAllowlistArgs {
                client_ip,
                user_payer,
            },
        );
        assert_eq!(remove_pub.data[0], 55);
        assert_eq!(remove_pub.accounts, remove_expected);

        let add_sub = add_multicast_group_sub_allowlist(
            &pid,
            &payer,
            &mgroup,
            &accesspass,
            AddMulticastGroupSubAllowlistArgs {
                client_ip,
                user_payer,
            },
        );
        assert_eq!(add_sub.data[0], 56);
        assert_eq!(add_sub.accounts, add_expected);

        let remove_sub = remove_multicast_group_sub_allowlist(
            &pid,
            &payer,
            &mgroup,
            &accesspass,
            RemoveMulticastGroupSubAllowlistArgs {
                client_ip,
                user_payer,
            },
        );
        assert_eq!(remove_sub.data[0], 57);
        assert_eq!(remove_sub.accounts, remove_expected);
    }
}
