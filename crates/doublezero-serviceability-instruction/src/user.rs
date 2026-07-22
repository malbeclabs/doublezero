//! User-domain instruction builders.

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalstate_pda, get_resource_extension_pda, get_user_pda},
    processors::user::{
        check_access_pass::CheckUserAccessPassArgs, create::UserCreateArgs,
        create_subscribe::UserCreateSubscribeArgs, delete::UserDeleteArgs,
        requestban::UserRequestBanArgs, set_bgp_status::SetUserBGPStatusArgs,
        update::UserUpdateArgs,
    },
    resource::ResourceType,
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// `CreateSubscribeUser` (variant 59).
///
/// Account layout (processor `next_account_info` order), before the trailing
/// `[payer, system]` appended by `common::build_with_permission`:
///
/// ```text
/// user                        (writable)  — get_user_pda(client_ip, user_type)
/// device                      (writable)
/// mgroup                      (writable)
/// accesspass                  (writable)
/// globalstate                 (writable)
/// user_tunnel_block           (writable)  — ResourceType::UserTunnelBlock
/// multicast_publisher_block   (writable)  — ResourceType::MulticastPublisherBlock
/// device_tunnel_ids           (writable)  — ResourceType::TunnelIds(device, 0)
/// dz_prefix_block[i]          (writable)  — one per dz_prefix_count
/// feed                        (readonly)  — OPTIONAL, appended only when Some
/// ```
///
/// `CreateSubscribeUser` is in the **`split_trailing_permission`** family (it
/// routes through `authorize()` for the USER_ADMIN owner-override), NOT the
/// length-detected family. The optional `feed` sits in the leading slice (before
/// `[payer, system]`); a Permission account, once activated, is appended *after*
/// `[payer, system]` and the processor peels it by PDA match — so the two never
/// collide. This builder is therefore assigned to `common::build_with_permission`
/// (permission deferred for now). `dz_prefix_count` is written back into the args
/// so it always matches the number of `dz_prefix_block` accounts produced.
#[allow(clippy::too_many_arguments)]
pub fn create_subscribe_user(
    program_id: &Pubkey,
    payer: &Pubkey,
    device: &Pubkey,
    mgroup: &Pubkey,
    accesspass: &Pubkey,
    dz_prefix_count: u8,
    feed: Option<&Pubkey>,
    mut args: UserCreateSubscribeArgs,
) -> Instruction {
    // The processor rejects `dz_prefix_count == 0` as its first statement
    // (create_subscribe.rs) — CreateSubscribeUser requires on-chain allocation — so a
    // zero here can only ever fail. Catch it cheaply in debug builds.
    debug_assert!(
        dz_prefix_count > 0,
        "dz_prefix_count must be > 0; CreateSubscribeUser requires on-chain allocation"
    );
    // The write-back overwrites any caller-set `dz_prefix_count`; assert they agree
    // (or the caller left it zero) to catch confusion cheaply in debug builds.
    debug_assert!(
        args.dz_prefix_count == 0 || args.dz_prefix_count == dz_prefix_count,
        "caller-set dz_prefix_count {} disagrees with {dz_prefix_count}",
        args.dz_prefix_count
    );
    args.dz_prefix_count = dz_prefix_count;

    let (user, _) = get_user_pda(program_id, &args.client_ip, args.user_type);
    let (globalstate, _) = get_globalstate_pda(program_id);
    let (user_tunnel_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::UserTunnelBlock);
    let (multicast_publisher_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::MulticastPublisherBlock);
    let (device_tunnel_ids, _, _) =
        get_resource_extension_pda(program_id, ResourceType::TunnelIds(*device, 0));

    let mut accounts = vec![
        AccountMeta::new(user, false),
        AccountMeta::new(*device, false),
        AccountMeta::new(*mgroup, false),
        AccountMeta::new(*accesspass, false),
        AccountMeta::new(globalstate, false),
        AccountMeta::new(user_tunnel_block, false),
        AccountMeta::new(multicast_publisher_block, false),
        AccountMeta::new(device_tunnel_ids, false),
    ];

    for idx in 0..dz_prefix_count as usize {
        let (dz_prefix, _, _) =
            get_resource_extension_pda(program_id, ResourceType::DzPrefixBlock(*device, idx));
        accounts.push(AccountMeta::new(dz_prefix, false));
    }

    // Optional trailing Feed account (EdgeSeat metro gate), appended BEFORE
    // payer/system. A Permission PDA, once activated, is appended AFTER payer/system
    // (not after the feed) and the processor peels it by PDA match, so the feed and
    // the Permission account never collide.
    if let Some(feed) = feed {
        accounts.push(AccountMeta::new_readonly(*feed, false));
    }

    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CreateSubscribeUser(args),
        accounts,
        payer,
    )
}

/// `CreateUser` (variant 36).
///
/// Account layout (processor `next_account_info` order), before the trailing
/// `[payer, system]` appended by [`common::build`]:
///
/// ```text
/// user                        (writable)  — get_user_pda(client_ip, user_type)
/// device                      (writable)
/// accesspass                  (writable)
/// globalstate                 (writable)
/// user_tunnel_block           (writable)  — ResourceType::UserTunnelBlock
/// multicast_publisher_block   (writable)  — ResourceType::MulticastPublisherBlock
/// device_tunnel_ids           (writable)  — ResourceType::TunnelIds(device, 0)
/// dz_prefix_block[i]          (writable)  — one per dz_prefix_count
/// tenant                      (writable)  — OPTIONAL, only when Some and non-default
/// ```
///
/// `CreateUser` is the genuine **length-detected** instruction: its processor
/// identifies the optional trailing `tenant` account via `accounts.len()` and
/// never calls `authorize()`. It is therefore assigned to [`common::build`] — a
/// Permission account must never be appended, or the count is corrupted.
pub fn create_user(
    program_id: &Pubkey,
    payer: &Pubkey,
    device: &Pubkey,
    accesspass: &Pubkey,
    dz_prefix_count: u8,
    tenant: Option<Pubkey>,
    mut args: UserCreateArgs,
) -> Instruction {
    // The processor rejects `dz_prefix_count == 0` as its first statement (create.rs)
    // — CreateUser requires on-chain allocation — so a zero here can only ever fail.
    // Catch it cheaply in debug builds.
    debug_assert!(
        dz_prefix_count > 0,
        "dz_prefix_count must be > 0; CreateUser requires on-chain allocation"
    );
    args.dz_prefix_count = dz_prefix_count;

    let (user, _) = get_user_pda(program_id, &args.client_ip, args.user_type);
    let (globalstate, _) = get_globalstate_pda(program_id);
    let (user_tunnel_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::UserTunnelBlock);
    let (multicast_publisher_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::MulticastPublisherBlock);
    let (device_tunnel_ids, _, _) =
        get_resource_extension_pda(program_id, ResourceType::TunnelIds(*device, 0));

    let mut accounts = vec![
        AccountMeta::new(user, false),
        AccountMeta::new(*device, false),
        AccountMeta::new(*accesspass, false),
        AccountMeta::new(globalstate, false),
        AccountMeta::new(user_tunnel_block, false),
        AccountMeta::new(multicast_publisher_block, false),
        AccountMeta::new(device_tunnel_ids, false),
    ];
    for idx in 0..dz_prefix_count as usize {
        let (dz_prefix, _, _) =
            get_resource_extension_pda(program_id, ResourceType::DzPrefixBlock(*device, idx));
        accounts.push(AccountMeta::new(dz_prefix, false));
    }
    if let Some(tenant) = tenant {
        if tenant != Pubkey::default() {
            accounts.push(AccountMeta::new(tenant, false));
        }
    }

    common::build(
        program_id,
        DoubleZeroInstruction::CreateUser(args),
        accounts,
        payer,
    )
}

/// `UpdateUser` (variant 39).
///
/// Account layout, before the trailing accounts:
///
/// ```text
/// user                        (writable)
/// globalstate                 (writable)
/// user_tunnel_block           (writable)  — ResourceType::UserTunnelBlock
/// multicast_publisher_block   (writable)  — ResourceType::MulticastPublisherBlock
/// device_tunnel_ids           (writable)  — ResourceType::TunnelIds(device, 0)
/// dz_prefix_block[i]          (writable)  — one per dz_prefix_count
/// // only when args.tenant_pk.is_some():
/// old_tenant                  (readonly when default, else writable) — user.tenant_pk
/// new_tenant                  (writable)  — args.tenant_pk
/// ```
///
/// `args.dz_prefix_count` is written from `dz_prefix_count`. Routes through
/// `authorize()` via `split_trailing_permission` -> [`common::build_with_permission`].
pub fn update_user(
    program_id: &Pubkey,
    payer: &Pubkey,
    user: &Pubkey,
    device: &Pubkey,
    dz_prefix_count: u8,
    old_tenant: &Pubkey,
    mut args: UserUpdateArgs,
) -> Instruction {
    // The processor rejects `dz_prefix_count == 0` as its first statement
    // (update.rs) — UpdateUser requires on-chain allocation — so a zero here can only
    // ever fail. Catch it cheaply in debug builds.
    debug_assert!(
        dz_prefix_count > 0,
        "dz_prefix_count must be > 0; UpdateUser requires on-chain allocation"
    );
    args.dz_prefix_count = dz_prefix_count;
    // This builder always emits the `multicast_publisher_block` account, so the
    // declared count MUST be > 0 or the processor (which reads that account only
    // when `multicast_publisher_count > 0`) would skip it and misread every
    // following account. Written back here for the same reason as
    // `dz_prefix_count`: keep the declared count and the account list in lockstep.
    args.multicast_publisher_count = 1;

    let (globalstate, _) = get_globalstate_pda(program_id);
    let (user_tunnel_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::UserTunnelBlock);
    let (multicast_publisher_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::MulticastPublisherBlock);
    let (device_tunnel_ids, _, _) =
        get_resource_extension_pda(program_id, ResourceType::TunnelIds(*device, 0));

    let mut accounts = vec![
        AccountMeta::new(*user, false),
        AccountMeta::new(globalstate, false),
        AccountMeta::new(user_tunnel_block, false),
        AccountMeta::new(multicast_publisher_block, false),
        AccountMeta::new(device_tunnel_ids, false),
    ];
    for idx in 0..dz_prefix_count as usize {
        let (dz_prefix, _, _) =
            get_resource_extension_pda(program_id, ResourceType::DzPrefixBlock(*device, idx));
        accounts.push(AccountMeta::new(dz_prefix, false));
    }
    // Reference-count adjustment on tenant change: old (readonly when default so the
    // runtime accepts it; the processor skips a default key) then new.
    if let Some(new_tenant) = args.tenant_pk {
        if *old_tenant == Pubkey::default() {
            accounts.push(AccountMeta::new_readonly(*old_tenant, false));
        } else {
            accounts.push(AccountMeta::new(*old_tenant, false));
        }
        accounts.push(AccountMeta::new(new_tenant, false));
    }

    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::UpdateUser(args),
        accounts,
        payer,
    )
}

/// `DeleteUser` (variant 42).
///
/// Account layout, before the trailing accounts:
///
/// ```text
/// user                        (writable)
/// accesspass                  (writable)
/// globalstate                 (writable)
/// device                      (writable)
/// user_tunnel_block           (writable)  — ResourceType::UserTunnelBlock
/// multicast_publisher_block   (writable)  — ResourceType::MulticastPublisherBlock
/// device_tunnel_ids           (writable)  — ResourceType::TunnelIds(device, 0)
/// dz_prefix_block[i]          (writable)  — one per dz_prefix_count
/// tenant                      (writable)  — OPTIONAL, only when non-default
/// owner                       (writable)  — user.owner
/// ```
///
/// `DeleteUser` detects its optional tenant from onchain state (not `accounts.len()`)
/// and calls `authorize()` positionally, so it routes to
/// [`common::build_with_permission`].
#[allow(clippy::too_many_arguments)]
pub fn delete_user(
    program_id: &Pubkey,
    payer: &Pubkey,
    user: &Pubkey,
    accesspass: &Pubkey,
    device: &Pubkey,
    dz_prefix_count: u8,
    tenant: Option<Pubkey>,
    owner: &Pubkey,
    mut args: UserDeleteArgs,
) -> Instruction {
    // The processor rejects `dz_prefix_count == 0` as its first statement
    // (delete.rs) — DeleteUser requires on-chain deallocation — so a zero here can
    // only ever fail. Catch it cheaply in debug builds.
    debug_assert!(
        dz_prefix_count > 0,
        "dz_prefix_count must be > 0; DeleteUser requires on-chain deallocation"
    );
    args.dz_prefix_count = dz_prefix_count;
    // This builder always emits the `multicast_publisher_block` account, so the
    // declared count MUST be > 0 or the processor (which reads that account only
    // when `multicast_publisher_count > 0`) would skip it and misread every
    // following account. Written back here for the same reason as
    // `dz_prefix_count`: keep the declared count and the account list in lockstep.
    args.multicast_publisher_count = 1;

    let (globalstate, _) = get_globalstate_pda(program_id);
    let (user_tunnel_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::UserTunnelBlock);
    let (multicast_publisher_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::MulticastPublisherBlock);
    let (device_tunnel_ids, _, _) =
        get_resource_extension_pda(program_id, ResourceType::TunnelIds(*device, 0));

    let mut accounts = vec![
        AccountMeta::new(*user, false),
        AccountMeta::new(*accesspass, false),
        AccountMeta::new(globalstate, false),
        AccountMeta::new(*device, false),
        AccountMeta::new(user_tunnel_block, false),
        AccountMeta::new(multicast_publisher_block, false),
        AccountMeta::new(device_tunnel_ids, false),
    ];
    for idx in 0..dz_prefix_count as usize {
        let (dz_prefix, _, _) =
            get_resource_extension_pda(program_id, ResourceType::DzPrefixBlock(*device, idx));
        accounts.push(AccountMeta::new(dz_prefix, false));
    }
    if let Some(tenant) = tenant {
        if tenant != Pubkey::default() {
            accounts.push(AccountMeta::new(tenant, false));
        }
    }
    accounts.push(AccountMeta::new(*owner, false));

    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::DeleteUser(args),
        accounts,
        payer,
    )
}

/// `RequestBanUser` (variant 44).
///
/// Account layout, before the trailing accounts:
///
/// ```text
/// user                        (writable)
/// globalstate                 (writable)
/// user_tunnel_block           (writable)  — ResourceType::UserTunnelBlock
/// multicast_publisher_block   (writable)  — ResourceType::MulticastPublisherBlock
/// device_tunnel_ids           (writable)  — ResourceType::TunnelIds(device, 0)
/// dz_prefix_block[i]          (writable)  — one per dz_prefix_count
/// ```
pub fn request_ban_user(
    program_id: &Pubkey,
    payer: &Pubkey,
    user: &Pubkey,
    device: &Pubkey,
    dz_prefix_count: u8,
    mut args: UserRequestBanArgs,
) -> Instruction {
    // The processor rejects `dz_prefix_count == 0` as its first statement
    // (requestban.rs) — RequestBanUser requires on-chain deallocation — so a zero here
    // can only ever fail. Catch it cheaply in debug builds.
    debug_assert!(
        dz_prefix_count > 0,
        "dz_prefix_count must be > 0; RequestBanUser requires on-chain deallocation"
    );
    args.dz_prefix_count = dz_prefix_count;
    // This builder always emits the `multicast_publisher_block` account, so the
    // declared count MUST be > 0 or the processor (which reads that account only
    // when `multicast_publisher_count > 0`) would skip it and misread every
    // following account. Written back here for the same reason as
    // `dz_prefix_count`: keep the declared count and the account list in lockstep.
    args.multicast_publisher_count = 1;

    let (globalstate, _) = get_globalstate_pda(program_id);
    let (user_tunnel_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::UserTunnelBlock);
    let (multicast_publisher_block, _, _) =
        get_resource_extension_pda(program_id, ResourceType::MulticastPublisherBlock);
    let (device_tunnel_ids, _, _) =
        get_resource_extension_pda(program_id, ResourceType::TunnelIds(*device, 0));

    let mut accounts = vec![
        AccountMeta::new(*user, false),
        AccountMeta::new(globalstate, false),
        AccountMeta::new(user_tunnel_block, false),
        AccountMeta::new(multicast_publisher_block, false),
        AccountMeta::new(device_tunnel_ids, false),
    ];
    for idx in 0..dz_prefix_count as usize {
        let (dz_prefix, _, _) =
            get_resource_extension_pda(program_id, ResourceType::DzPrefixBlock(*device, idx));
        accounts.push(AccountMeta::new(dz_prefix, false));
    }

    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::RequestBanUser(args),
        accounts,
        payer,
    )
}

/// `CheckUserAccessPass` (variant 71).
///
/// Account layout, before the trailing accounts:
///
/// ```text
/// user         (writable)
/// accesspass   (writable)  — get_accesspass_pda(user.client_ip, user.owner)
/// globalstate  (writable)
/// ```
pub fn check_user_access_pass(
    program_id: &Pubkey,
    payer: &Pubkey,
    user: &Pubkey,
    accesspass: &Pubkey,
    args: CheckUserAccessPassArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    let accounts = vec![
        AccountMeta::new(*user, false),
        AccountMeta::new(*accesspass, false),
        AccountMeta::new(globalstate, false),
    ];
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CheckUserAccessPass(args),
        accounts,
        payer,
    )
}

/// `SetUserBGPStatus` (variant 106).
///
/// Account layout, before the trailing accounts:
///
/// ```text
/// user    (writable)
/// device  (readonly)
/// ```
///
/// The processor authorizes by checking that the payer equals the device's
/// `metrics_publisher_pk` — it does NOT call `authorize()`, so this is assigned to
/// [`common::build`] (no Permission account). Note there is no globalstate account.
pub fn set_user_bgp_status(
    program_id: &Pubkey,
    payer: &Pubkey,
    user: &Pubkey,
    device: &Pubkey,
    args: SetUserBGPStatusArgs,
) -> Instruction {
    let accounts = vec![
        AccountMeta::new(*user, false),
        AccountMeta::new_readonly(*device, false),
    ];
    common::build(
        program_id,
        DoubleZeroInstruction::SetUserBGPStatus(args),
        accounts,
        payer,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_serviceability::state::user::{BGPStatus, UserCYOA, UserType};
    use solana_system_interface::program as system_program;
    use std::net::Ipv4Addr;

    fn base_args(client_ip: Ipv4Addr) -> UserCreateSubscribeArgs {
        UserCreateSubscribeArgs {
            user_type: UserType::IBRLWithAllocatedIP,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            publisher: true,
            subscriber: false,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 0,
            owner: Pubkey::default(),
        }
    }

    #[test]
    fn test_create_subscribe_user_no_feed() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let mgroup = Pubkey::new_unique();
        let accesspass = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let args = base_args(client_ip);
        let ix = create_subscribe_user(
            &pid,
            &payer,
            &device,
            &mgroup,
            &accesspass,
            1,
            None,
            args.clone(),
        );

        assert_eq!(ix.data[0], 59);
        // Assert the full args round-trips: the builder's `mut args` write-back
        // must touch ONLY `dz_prefix_count`, and future fields are pinned too.
        let expected = UserCreateSubscribeArgs {
            dz_prefix_count: 1,
            ..args
        };
        let decoded = DoubleZeroInstruction::unpack(&ix.data).unwrap();
        match decoded {
            DoubleZeroInstruction::CreateSubscribeUser(a) => assert_eq!(a, expected),
            other => panic!("unexpected variant: {other:?}"),
        }

        let (user, _) = get_user_pda(&pid, &client_ip, UserType::IBRLWithAllocatedIP);
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (user_tunnel_block, _, _) =
            get_resource_extension_pda(&pid, ResourceType::UserTunnelBlock);
        let (multicast_publisher_block, _, _) =
            get_resource_extension_pda(&pid, ResourceType::MulticastPublisherBlock);
        let (device_tunnel_ids, _, _) =
            get_resource_extension_pda(&pid, ResourceType::TunnelIds(device, 0));
        let (dz_prefix0, _, _) =
            get_resource_extension_pda(&pid, ResourceType::DzPrefixBlock(device, 0));

        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(user, false),
                AccountMeta::new(device, false),
                AccountMeta::new(mgroup, false),
                AccountMeta::new(accesspass, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(user_tunnel_block, false),
                AccountMeta::new(multicast_publisher_block, false),
                AccountMeta::new(device_tunnel_ids, false),
                AccountMeta::new(dz_prefix0, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_create_subscribe_user_with_feed_is_readonly_and_last_before_trailing() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let feed = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let ix = create_subscribe_user(
            &pid,
            &payer,
            &device,
            &Pubkey::new_unique(),
            &Pubkey::new_unique(),
            1,
            Some(&feed),
            base_args(client_ip),
        );

        // 8 fixed + 1 dz_prefix + feed + payer + system = 12.
        assert_eq!(ix.accounts.len(), 12);
        // Feed sits right before the payer/system trailing pair, read-only.
        let feed_meta = &ix.accounts[ix.accounts.len() - 3];
        assert_eq!(feed_meta.pubkey, feed);
        assert!(!feed_meta.is_writable);
        assert!(!feed_meta.is_signer);
        // Permission append is deferred (build_with_permission delegates to build
        // today), so the last account is the system program.
        assert_eq!(ix.accounts.last().unwrap().pubkey, system_program::ID);
    }

    fn create_args(client_ip: Ipv4Addr) -> UserCreateArgs {
        UserCreateArgs {
            user_type: UserType::IBRLWithAllocatedIP,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 0,
        }
    }

    #[test]
    fn test_create_user_length_detected_no_permission() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let accesspass = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let ix = create_user(
            &pid,
            &payer,
            &device,
            &accesspass,
            1,
            None,
            create_args(client_ip),
        );
        assert_eq!(ix.data[0], 36);
        let (user, _) = get_user_pda(&pid, &client_ip, UserType::IBRLWithAllocatedIP);
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (utb, _, _) = get_resource_extension_pda(&pid, ResourceType::UserTunnelBlock);
        let (mpb, _, _) = get_resource_extension_pda(&pid, ResourceType::MulticastPublisherBlock);
        let (dti, _, _) = get_resource_extension_pda(&pid, ResourceType::TunnelIds(device, 0));
        let (dz0, _, _) = get_resource_extension_pda(&pid, ResourceType::DzPrefixBlock(device, 0));
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(user, false),
                AccountMeta::new(device, false),
                AccountMeta::new(accesspass, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(utb, false),
                AccountMeta::new(mpb, false),
                AccountMeta::new(dti, false),
                AccountMeta::new(dz0, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
        // build() path: no Permission account, ever.
        assert_eq!(ix.accounts.last().unwrap().pubkey, system_program::ID);
    }

    #[test]
    fn test_create_user_appends_non_default_tenant_before_trailing() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let tenant = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let ix = create_user(
            &pid,
            &payer,
            &device,
            &Pubkey::new_unique(),
            1,
            Some(tenant),
            create_args(client_ip),
        );
        // 7 fixed + 1 dz_prefix + tenant + payer + system = 11; tenant precedes payer.
        assert_eq!(ix.accounts.len(), 11);
        assert_eq!(ix.accounts[ix.accounts.len() - 3].pubkey, tenant);
        assert!(ix.accounts[ix.accounts.len() - 3].is_writable);

        // A default tenant is never appended.
        let ix2 = create_user(
            &pid,
            &payer,
            &device,
            &Pubkey::new_unique(),
            1,
            Some(Pubkey::default()),
            create_args(client_ip),
        );
        assert_eq!(ix2.accounts.len(), 10);
    }

    #[test]
    fn test_update_user_with_tenant_change() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let user = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let new_tenant = Pubkey::new_unique();

        let args = UserUpdateArgs {
            tenant_pk: Some(new_tenant),
            ..Default::default()
        };
        // old_tenant default -> appended read-only before new_tenant.
        let ix = update_user(&pid, &payer, &user, &device, 1, &Pubkey::default(), args);
        assert_eq!(ix.data[0], 39);
        // The builder always emits the mpb account, so it MUST pin
        // multicast_publisher_count > 0 to keep the declared count and the
        // account list in lockstep (else the processor skips the mpb slot and
        // misreads every following account).
        match DoubleZeroInstruction::unpack(&ix.data).unwrap() {
            DoubleZeroInstruction::UpdateUser(a) => assert_eq!(a.multicast_publisher_count, 1),
            other => panic!("unexpected variant: {other:?}"),
        }
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (utb, _, _) = get_resource_extension_pda(&pid, ResourceType::UserTunnelBlock);
        let (mpb, _, _) = get_resource_extension_pda(&pid, ResourceType::MulticastPublisherBlock);
        let (dti, _, _) = get_resource_extension_pda(&pid, ResourceType::TunnelIds(device, 0));
        let (dz0, _, _) = get_resource_extension_pda(&pid, ResourceType::DzPrefixBlock(device, 0));
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(user, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(utb, false),
                AccountMeta::new(mpb, false),
                AccountMeta::new(dti, false),
                AccountMeta::new(dz0, false),
                AccountMeta::new_readonly(Pubkey::default(), false),
                AccountMeta::new(new_tenant, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_update_user_with_non_default_old_tenant_is_writable() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let user = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let old_tenant = Pubkey::new_unique();
        let new_tenant = Pubkey::new_unique();

        let args = UserUpdateArgs {
            tenant_pk: Some(new_tenant),
            ..Default::default()
        };
        // old_tenant non-default -> appended WRITABLE (ref-count decrement) before new_tenant.
        let ix = update_user(&pid, &payer, &user, &device, 1, &old_tenant, args);
        assert_eq!(ix.data[0], 39);
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (utb, _, _) = get_resource_extension_pda(&pid, ResourceType::UserTunnelBlock);
        let (mpb, _, _) = get_resource_extension_pda(&pid, ResourceType::MulticastPublisherBlock);
        let (dti, _, _) = get_resource_extension_pda(&pid, ResourceType::TunnelIds(device, 0));
        let (dz0, _, _) = get_resource_extension_pda(&pid, ResourceType::DzPrefixBlock(device, 0));
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(user, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(utb, false),
                AccountMeta::new(mpb, false),
                AccountMeta::new(dti, false),
                AccountMeta::new(dz0, false),
                AccountMeta::new(old_tenant, false),
                AccountMeta::new(new_tenant, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_update_user_no_tenant_change() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let user = Pubkey::new_unique();
        let device = Pubkey::new_unique();

        let ix = update_user(
            &pid,
            &payer,
            &user,
            &device,
            1,
            &Pubkey::new_unique(),
            UserUpdateArgs::default(),
        );
        // 5 fixed + 1 dz_prefix + payer + system = 8 (no tenant accounts).
        assert_eq!(ix.accounts.len(), 8);
    }

    #[test]
    fn test_delete_user() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let user = Pubkey::new_unique();
        let accesspass = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let owner = Pubkey::new_unique();

        let ix = delete_user(
            &pid,
            &payer,
            &user,
            &accesspass,
            &device,
            1,
            None,
            &owner,
            UserDeleteArgs::default(),
        );
        assert_eq!(ix.data[0], 42);
        // The builder always emits the mpb account, so it MUST pin
        // multicast_publisher_count > 0 to keep the declared count and the
        // account list in lockstep (else the processor skips the mpb slot and
        // misreads every following account).
        match DoubleZeroInstruction::unpack(&ix.data).unwrap() {
            DoubleZeroInstruction::DeleteUser(a) => assert_eq!(a.multicast_publisher_count, 1),
            other => panic!("unexpected variant: {other:?}"),
        }
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (utb, _, _) = get_resource_extension_pda(&pid, ResourceType::UserTunnelBlock);
        let (mpb, _, _) = get_resource_extension_pda(&pid, ResourceType::MulticastPublisherBlock);
        let (dti, _, _) = get_resource_extension_pda(&pid, ResourceType::TunnelIds(device, 0));
        let (dz0, _, _) = get_resource_extension_pda(&pid, ResourceType::DzPrefixBlock(device, 0));
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(user, false),
                AccountMeta::new(accesspass, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(device, false),
                AccountMeta::new(utb, false),
                AccountMeta::new(mpb, false),
                AccountMeta::new(dti, false),
                AccountMeta::new(dz0, false),
                AccountMeta::new(owner, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_delete_user_with_non_default_tenant() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let user = Pubkey::new_unique();
        let accesspass = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let tenant = Pubkey::new_unique();
        let owner = Pubkey::new_unique();

        // Non-default tenant -> a writable tenant account is inserted between the
        // dz_prefix accounts and owner.
        let ix = delete_user(
            &pid,
            &payer,
            &user,
            &accesspass,
            &device,
            1,
            Some(tenant),
            &owner,
            UserDeleteArgs::default(),
        );
        assert_eq!(ix.data[0], 42);
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (utb, _, _) = get_resource_extension_pda(&pid, ResourceType::UserTunnelBlock);
        let (mpb, _, _) = get_resource_extension_pda(&pid, ResourceType::MulticastPublisherBlock);
        let (dti, _, _) = get_resource_extension_pda(&pid, ResourceType::TunnelIds(device, 0));
        let (dz0, _, _) = get_resource_extension_pda(&pid, ResourceType::DzPrefixBlock(device, 0));
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(user, false),
                AccountMeta::new(accesspass, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(device, false),
                AccountMeta::new(utb, false),
                AccountMeta::new(mpb, false),
                AccountMeta::new(dti, false),
                AccountMeta::new(dz0, false),
                AccountMeta::new(tenant, false),
                AccountMeta::new(owner, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );

        // A default tenant is never appended (owner directly follows dz_prefix).
        let ix2 = delete_user(
            &pid,
            &payer,
            &user,
            &accesspass,
            &device,
            1,
            Some(Pubkey::default()),
            &owner,
            UserDeleteArgs::default(),
        );
        assert_eq!(ix2.accounts.len(), 11);
        assert_eq!(ix2.accounts[ix2.accounts.len() - 3].pubkey, owner);
    }

    #[test]
    fn test_request_ban_user() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let user = Pubkey::new_unique();
        let device = Pubkey::new_unique();

        let ix = request_ban_user(
            &pid,
            &payer,
            &user,
            &device,
            1,
            UserRequestBanArgs::default(),
        );
        assert_eq!(ix.data[0], 44);
        // The builder always emits the mpb account, so it MUST pin
        // multicast_publisher_count > 0 to keep the declared count and the
        // account list in lockstep (else the processor skips the mpb slot and
        // misreads every following account).
        match DoubleZeroInstruction::unpack(&ix.data).unwrap() {
            DoubleZeroInstruction::RequestBanUser(a) => assert_eq!(a.multicast_publisher_count, 1),
            other => panic!("unexpected variant: {other:?}"),
        }
        let (globalstate, _) = get_globalstate_pda(&pid);
        let (utb, _, _) = get_resource_extension_pda(&pid, ResourceType::UserTunnelBlock);
        let (mpb, _, _) = get_resource_extension_pda(&pid, ResourceType::MulticastPublisherBlock);
        let (dti, _, _) = get_resource_extension_pda(&pid, ResourceType::TunnelIds(device, 0));
        let (dz0, _, _) = get_resource_extension_pda(&pid, ResourceType::DzPrefixBlock(device, 0));
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(user, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(utb, false),
                AccountMeta::new(mpb, false),
                AccountMeta::new(dti, false),
                AccountMeta::new(dz0, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_check_user_access_pass() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let user = Pubkey::new_unique();
        let accesspass = Pubkey::new_unique();

        let ix = check_user_access_pass(
            &pid,
            &payer,
            &user,
            &accesspass,
            CheckUserAccessPassArgs::default(),
        );
        assert_eq!(ix.data[0], 71);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(user, false),
                AccountMeta::new(accesspass, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_set_user_bgp_status_no_permission() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let user = Pubkey::new_unique();
        let device = Pubkey::new_unique();

        let args = SetUserBGPStatusArgs {
            bgp_status: BGPStatus::Up,
            bgp_rtt_ns: 12_345,
        };
        let ix = set_user_bgp_status(&pid, &payer, &user, &device, args);
        assert_eq!(ix.data[0], 106);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(user, false),
                AccountMeta::new_readonly(device, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }
}
