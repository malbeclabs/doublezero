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
        suspend::MulticastGroupSuspendArgs,
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

/// `AddMulticastGroupPubAllowlist` (variant 54).
/// Accounts: `[mgroup, accesspass, globalstate, user_payer]`.
pub fn add_multicast_group_pub_allowlist(
    program_id: &Pubkey,
    payer: &Pubkey,
    mgroup: &Pubkey,
    accesspass: &Pubkey,
    user_payer: &Pubkey,
    args: AddMulticastGroupPubAllowlistArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(args),
        vec![
            AccountMeta::new(*mgroup, false),
            AccountMeta::new(*accesspass, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(*user_payer, false),
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
    user_payer: &Pubkey,
    args: AddMulticastGroupSubAllowlistArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::AddMulticastGroupSubAllowlist(args),
        vec![
            AccountMeta::new(*mgroup, false),
            AccountMeta::new(*accesspass, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(*user_payer, false),
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
        assert_eq!(without_ip.accounts.len(), 4);
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
            &user_payer,
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
            &user_payer,
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
