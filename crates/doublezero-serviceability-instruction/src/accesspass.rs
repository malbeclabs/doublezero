//! Access-pass-domain instruction builders.
//!
//! All route through `authorize()` -> [`common::build_with_permission`]. The
//! access-pass PDA is derived from `(client_ip, user_payer)`.

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_accesspass_pda, get_globalstate_pda},
    processors::accesspass::{
        check_status::CheckStatusAccessPassArgs, close::CloseAccessPassArgs,
        set::SetAccessPassArgs, set_feeds::SetAccessPassFeedsArgs,
    },
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};
use std::net::Ipv4Addr;

/// `SetAccessPass` (variant 67).
/// Accounts: `[accesspass, globalstate(readonly), user_payer]`, plus
/// `[current_tenant, new_tenant]` when either is non-default.
///
/// The access-pass PDA is derived from `args.client_ip` and `user_payer`.
/// `current_tenant` is the tenant currently on the pass (onchain-read).
pub fn set_access_pass(
    program_id: &Pubkey,
    payer: &Pubkey,
    user_payer: &Pubkey,
    current_tenant: &Pubkey,
    new_tenant: &Pubkey,
    args: SetAccessPassArgs,
) -> Instruction {
    let (accesspass, _) = get_accesspass_pda(program_id, &args.client_ip, user_payer);
    let (globalstate, _) = get_globalstate_pda(program_id);
    let mut accounts = vec![
        AccountMeta::new(accesspass, false),
        AccountMeta::new_readonly(globalstate, false),
        AccountMeta::new(*user_payer, false),
    ];
    if *current_tenant != Pubkey::default() || *new_tenant != Pubkey::default() {
        accounts.push(AccountMeta::new(*current_tenant, false));
        accounts.push(AccountMeta::new(*new_tenant, false));
    }
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::SetAccessPass(args),
        accounts,
        payer,
    )
}

/// `CloseAccessPass` (variant 69). Accounts: `[accesspass, globalstate]`.
pub fn close_access_pass(
    program_id: &Pubkey,
    payer: &Pubkey,
    accesspass: &Pubkey,
    args: CloseAccessPassArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CloseAccessPass(args),
        vec![
            AccountMeta::new(*accesspass, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `CheckStatusAccessPass` (variant 70).
/// Accounts: `[accesspass, globalstate(readonly)]`.
///
/// The access-pass PDA is derived from `(client_ip, user_payer)`; the args carry
/// no fields, so both are explicit parameters.
pub fn check_status_access_pass(
    program_id: &Pubkey,
    payer: &Pubkey,
    client_ip: Ipv4Addr,
    user_payer: &Pubkey,
    args: CheckStatusAccessPassArgs,
) -> Instruction {
    let (accesspass, _) = get_accesspass_pda(program_id, &client_ip, user_payer);
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CheckStatusAccessPass(args),
        vec![
            AccountMeta::new(accesspass, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        payer,
    )
}

/// `SetAccessPassFeeds` (variant 115).
/// Accounts: `[accesspass, globalstate(readonly), feed[i]...(readonly)]`.
///
/// The access-pass PDA is derived from `args.client_ip` and `args.user_payer`.
/// `feed_keys` are the feed accounts, in the same order as `args.feeds`.
pub fn set_access_pass_feeds(
    program_id: &Pubkey,
    payer: &Pubkey,
    feed_keys: &[Pubkey],
    args: SetAccessPassFeedsArgs,
) -> Instruction {
    let (accesspass, _) = get_accesspass_pda(program_id, &args.client_ip, &args.user_payer);
    let (globalstate, _) = get_globalstate_pda(program_id);
    let mut accounts = vec![
        AccountMeta::new(accesspass, false),
        AccountMeta::new_readonly(globalstate, false),
    ];
    for feed in feed_keys {
        accounts.push(AccountMeta::new_readonly(*feed, false));
    }
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::SetAccessPassFeeds(args),
        accounts,
        payer,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_serviceability::state::accesspass::AccessPassType;
    use solana_system_interface::program as system_program;

    fn set_args(client_ip: Ipv4Addr) -> SetAccessPassArgs {
        SetAccessPassArgs {
            accesspass_type: AccessPassType::default(),
            client_ip,
            last_access_epoch: 0,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }
    }

    #[test]
    fn test_set_access_pass_no_tenant() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let user_payer = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(10, 0, 0, 1);
        let ix = set_access_pass(
            &pid,
            &payer,
            &user_payer,
            &Pubkey::default(),
            &Pubkey::default(),
            set_args(client_ip),
        );
        assert_eq!(ix.data[0], 67);
        let (accesspass, _) = get_accesspass_pda(&pid, &client_ip, &user_payer);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(accesspass, false),
                AccountMeta::new_readonly(globalstate, false),
                AccountMeta::new(user_payer, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_set_access_pass_with_tenant_appends_pair() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let user_payer = Pubkey::new_unique();
        let new_tenant = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(10, 0, 0, 1);
        let ix = set_access_pass(
            &pid,
            &payer,
            &user_payer,
            &Pubkey::default(),
            &new_tenant,
            set_args(client_ip),
        );
        // 3 base + current_tenant(default) + new_tenant + payer + system = 7.
        assert_eq!(ix.accounts.len(), 7);
        assert_eq!(ix.accounts[3].pubkey, Pubkey::default());
        assert_eq!(ix.accounts[4].pubkey, new_tenant);
    }

    #[test]
    fn test_close_and_check_status() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let accesspass = Pubkey::new_unique();
        let (globalstate, _) = get_globalstate_pda(&pid);

        let close = close_access_pass(&pid, &payer, &accesspass, CloseAccessPassArgs {});
        assert_eq!(close.data[0], 69);
        assert_eq!(
            close.accounts,
            vec![
                AccountMeta::new(accesspass, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );

        let user_payer = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(10, 0, 0, 1);
        let check = check_status_access_pass(
            &pid,
            &payer,
            client_ip,
            &user_payer,
            CheckStatusAccessPassArgs {},
        );
        assert_eq!(check.data[0], 70);
        let (ap, _) = get_accesspass_pda(&pid, &client_ip, &user_payer);
        assert_eq!(
            check.accounts,
            vec![
                AccountMeta::new(ap, false),
                AccountMeta::new_readonly(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_set_access_pass_feeds() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let user_payer = Pubkey::new_unique();
        let feed = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(10, 0, 0, 1);
        let args = SetAccessPassFeedsArgs {
            client_ip,
            user_payer,
            feeds: vec![],
        };
        let ix = set_access_pass_feeds(&pid, &payer, &[feed], args);
        assert_eq!(ix.data[0], 115);
        let (accesspass, _) = get_accesspass_pda(&pid, &client_ip, &user_payer);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(accesspass, false),
                AccountMeta::new_readonly(globalstate, false),
                AccountMeta::new_readonly(feed, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }
}
