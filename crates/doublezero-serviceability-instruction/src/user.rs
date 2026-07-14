//! User-domain instruction builders.

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalstate_pda, get_resource_extension_pda, get_user_pda},
    processors::user::create_subscribe::UserCreateSubscribeArgs,
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

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_serviceability::state::user::{UserCYOA, UserType};
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

        let ix = create_subscribe_user(
            &pid,
            &payer,
            &device,
            &mgroup,
            &accesspass,
            1,
            None,
            base_args(client_ip),
        );

        assert_eq!(ix.data[0], 59);
        // dz_prefix_count is written back into the args.
        let decoded = DoubleZeroInstruction::unpack(&ix.data).unwrap();
        match decoded {
            DoubleZeroInstruction::CreateSubscribeUser(a) => assert_eq!(a.dz_prefix_count, 1),
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
}
