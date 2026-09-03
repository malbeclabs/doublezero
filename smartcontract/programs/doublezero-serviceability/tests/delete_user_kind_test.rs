//! Issue #2470: each `Delete<Kind>User` instruction must delete a user whose access pass is of
//! its own kind, and refuse a user on a pass of any other kind with `InvalidAccessPassType`.

mod test_helpers;

use doublezero_serviceability::{
    error::DoubleZeroError,
    instructions::DoubleZeroInstruction,
    pda::{get_accesspass_pda, get_device_pda, get_resource_extension_pda, get_user_pda},
    processors::{
        accesspass::set::SetAccessPassArgs,
        device::{create::DeviceCreateArgs, update::DeviceUpdateArgs},
        user::{create::UserCreateArgs, delete::UserDeleteArgs},
    },
    resource::ResourceType,
    state::{
        accesspass::{AccessPass, AccessPassType, FeedSeat},
        device::{DeviceDesiredStatus, DeviceType},
        user::{User, UserCYOA, UserType},
    },
};
use solana_program_test::*;
use solana_sdk::{
    account::AccountSharedData, instruction::AccountMeta, pubkey::Pubkey, signature::Keypair,
    signer::Signer,
};
use std::net::Ipv4Addr;
use test_helpers::*;

fn delete_args() -> UserDeleteArgs {
    UserDeleteArgs {
        dz_prefix_count: 1,
        multicast_publisher_count: 1,
    }
}

/// The delete instruction that matches each pass type, and one that does not.
fn delete_instructions(
    pass_type: &AccessPassType,
) -> (DoubleZeroInstruction, DoubleZeroInstruction) {
    match pass_type {
        AccessPassType::Prepaid => (
            DoubleZeroInstruction::DeletePrepaidUser(delete_args()),
            DoubleZeroInstruction::DeleteEdgeSeatUser(delete_args()),
        ),
        AccessPassType::SolanaValidator(_) => (
            DoubleZeroInstruction::DeleteSolanaValidatorUser(delete_args()),
            DoubleZeroInstruction::DeletePrepaidUser(delete_args()),
        ),
        AccessPassType::SolanaRPC(_) => (
            DoubleZeroInstruction::DeleteSolanaRPCUser(delete_args()),
            DoubleZeroInstruction::DeletePrepaidUser(delete_args()),
        ),
        AccessPassType::Others(_, _) => (
            DoubleZeroInstruction::DeleteOthersUser(delete_args()),
            DoubleZeroInstruction::DeletePrepaidUser(delete_args()),
        ),
        AccessPassType::EdgeSeat(_) => (
            DoubleZeroInstruction::DeleteEdgeSeatUser(delete_args()),
            DoubleZeroInstruction::DeletePrepaidUser(delete_args()),
        ),
    }
}

struct TestEnv {
    context: ProgramTestContext,
    payer: Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    device_pubkey: Pubkey,
}

/// GlobalState/Config, Location, Exchange, Contributor and an Activated Device, ready to host
/// users under any access-pass kind.
async fn setup_test_env() -> TestEnv {
    let (mut context, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig_context().await;
    let payer = context.payer.insecure_clone();
    let recent_blockhash = context.last_blockhash;

    let (location_pubkey, exchange_pubkey, contributor_pubkey) = setup_device_prerequisites(
        &mut context.banks_client,
        recent_blockhash,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    let gs = get_globalstate(&mut context.banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, gs.account_index + 1);
    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

    execute_transaction(
        &mut context.banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "dev".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [100, 0, 0, 1].into(),
            dz_prefixes: "100.1.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 2,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut context.banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
            max_users: Some(128),
            ..DeviceUpdateArgs::default()
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    TestEnv {
        context,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
    }
}

/// Set an access pass of `pass_type` at `client_ip` and create a user of `user_type` under it.
/// Returns (accesspass_pubkey, user_pubkey).
async fn create_and_activate_user(
    env: &mut TestEnv,
    client_ip: Ipv4Addr,
    user_type: UserType,
    pass_type: AccessPassType,
) -> (Pubkey, Pubkey) {
    let recent_blockhash = env.context.last_blockhash;
    let payer_pk = env.payer.pubkey();

    let (accesspass_pubkey, _) = get_accesspass_pda(&env.program_id, &client_ip, &payer_pk);

    execute_transaction(
        &mut env.context.banks_client,
        recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: pass_type,
            client_ip,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(env.globalstate_pubkey, false),
            AccountMeta::new(payer_pk, false),
        ],
        &env.payer,
    )
    .await;

    let (user_pubkey, _) = get_user_pda(&env.program_id, &client_ip, user_type);
    let (user_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&env.program_id, ResourceType::UserTunnelBlock);
    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&env.program_id, ResourceType::MulticastPublisherBlock);
    let (device_tunnel_ids_pda, _, _) = get_resource_extension_pda(
        &env.program_id,
        ResourceType::TunnelIds(env.device_pubkey, 0),
    );
    let (dz_prefix_block_pda, _, _) = get_resource_extension_pda(
        &env.program_id,
        ResourceType::DzPrefixBlock(env.device_pubkey, 0),
    );

    execute_transaction(
        &mut env.context.banks_client,
        recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip,
            user_type,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
            ip_proof: None,
        }),
        vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(env.device_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(env.globalstate_pubkey, false),
            AccountMeta::new(user_tunnel_block_pda, false),
            AccountMeta::new(multicast_publisher_block_pda, false),
            AccountMeta::new(device_tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_block_pda, false),
        ],
        &env.payer,
    )
    .await;

    (accesspass_pubkey, user_pubkey)
}

/// Rewrite the EdgeSeat pass to carry one feed seat with a user on it, and give the user that
/// same feed in `feed_pks`, bypassing the real provisioning path. That real path is
/// `SetAccessPassFeeds` (see `set_access_pass_feeds_test.rs`), but it only puts the seat on the
/// pass — it needs a caller with a permissioned authority (foundation allowlist or
/// `ACCESS_PASS_ADMIN`) and does not tick `current_users` or touch a user. Recording the feed on
/// a user, ticked, is done by `CreateSubscribeUser` or `SubscribeFeed`, and both require a real
/// `MulticastGroup` (its own create instruction, `ResourceExtension` accounts, and onchain
/// allocation), which this suite does not otherwise set up. Standing that up here to seed one
/// feed seat would roughly double this file for no gain in what the delete path itself is
/// tested against, so the seat is seeded directly instead.
/// This is the only way to put a real seat in front of `process_delete_user`'s
/// `release_feed_seats` call without that extra machinery; a feedless EdgeSeat pass makes that
/// call a no-op and never exercises the release path a `DeleteEdgeSeatUser` must perform.
async fn seed_feed_seat(
    env: &mut TestEnv,
    accesspass_pubkey: Pubkey,
    user_pubkey: Pubkey,
) -> Pubkey {
    let feed_key = Pubkey::new_unique();

    let mut accesspass_account = env
        .context
        .banks_client
        .get_account(accesspass_pubkey)
        .await
        .unwrap()
        .expect("access pass must exist");
    let mut accesspass = AccessPass::try_from(&accesspass_account.data[..]).unwrap();
    accesspass.accesspass_type = AccessPassType::EdgeSeat(vec![FeedSeat {
        feed_key,
        max_users: 1,
        max_future_users: 1,
        current_users: 1,
        anniversary_day: 1,
        window_end: 4_000_000_000,
        terminates_at: 4_100_000_000,
    }]);
    accesspass_account.data = borsh::to_vec(&accesspass).unwrap();
    env.context.set_account(
        &accesspass_pubkey,
        &AccountSharedData::from(accesspass_account),
    );

    let mut user_account = env
        .context
        .banks_client
        .get_account(user_pubkey)
        .await
        .unwrap()
        .expect("user must exist");
    let mut user = User::try_from(&user_account.data[..]).unwrap();
    user.feed_pks = vec![feed_key];
    user_account.data = borsh::to_vec(&user).unwrap();
    env.context
        .set_account(&user_pubkey, &AccountSharedData::from(user_account));

    feed_key
}

#[tokio::test]
async fn delete_refuses_a_user_of_another_kind() {
    for (i, pass_type) in [
        AccessPassType::Prepaid,
        AccessPassType::SolanaValidator(Pubkey::new_unique()),
        AccessPassType::SolanaRPC(Pubkey::new_unique()),
        AccessPassType::Others("thing".to_string(), "key".to_string()),
        AccessPassType::EdgeSeat(vec![]),
    ]
    .into_iter()
    .enumerate()
    {
        let mut env = setup_test_env().await;
        let client_ip: Ipv4Addr = [100, 0, 0, 10 + i as u8].into();
        let user_type = if matches!(pass_type, AccessPassType::EdgeSeat(_)) {
            UserType::Multicast
        } else {
            UserType::IBRL
        };

        let (accesspass_pubkey, user_pubkey) =
            create_and_activate_user(&mut env, client_ip, user_type, pass_type.clone()).await;

        if matches!(pass_type, AccessPassType::EdgeSeat(_)) {
            seed_feed_seat(&mut env, accesspass_pubkey, user_pubkey).await;
        }

        let (matching, other) = delete_instructions(&pass_type);

        let (user_tunnel_block_pda, _, _) =
            get_resource_extension_pda(&env.program_id, ResourceType::UserTunnelBlock);
        let (multicast_publisher_block_pda, _, _) =
            get_resource_extension_pda(&env.program_id, ResourceType::MulticastPublisherBlock);
        let (device_tunnel_ids_pda, _, _) = get_resource_extension_pda(
            &env.program_id,
            ResourceType::TunnelIds(env.device_pubkey, 0),
        );
        let (dz_prefix_pda, _, _) = get_resource_extension_pda(
            &env.program_id,
            ResourceType::DzPrefixBlock(env.device_pubkey, 0),
        );
        let owner = env.payer.pubkey();

        let accounts = vec![
            AccountMeta::new(user_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(env.globalstate_pubkey, false),
            AccountMeta::new(env.device_pubkey, false),
            AccountMeta::new(user_tunnel_block_pda, false),
            AccountMeta::new(multicast_publisher_block_pda, false),
            AccountMeta::new(device_tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
            AccountMeta::new(owner, false),
        ];

        let err = try_execute_transaction(
            &mut env.context.banks_client,
            env.context.last_blockhash,
            env.program_id,
            other,
            accounts.clone(),
            &env.payer,
        )
        .await
        .expect_err("a delete for another kind must fail");
        assert_custom_error(&err, DoubleZeroError::InvalidAccessPassType);

        assert!(
            get_account_data(&mut env.context.banks_client, user_pubkey)
                .await
                .is_some(),
            "the user must survive a refused delete: {pass_type}"
        );

        execute_transaction(
            &mut env.context.banks_client,
            env.context.last_blockhash,
            env.program_id,
            matching,
            accounts,
            &env.payer,
        )
        .await;

        assert!(
            get_account_data(&mut env.context.banks_client, user_pubkey)
                .await
                .is_none(),
            "the matching delete must remove the user: {pass_type}"
        );

        // The EdgeSeat case seeded a real feed seat (see `seed_feed_seat`); confirm the matching
        // delete actually released it rather than `release_feed_seats` silently no-op'ing.
        if matches!(pass_type, AccessPassType::EdgeSeat(_)) {
            let accesspass_account = env
                .context
                .banks_client
                .get_account(accesspass_pubkey)
                .await
                .unwrap()
                .expect("access pass survives a user delete");
            let accesspass = AccessPass::try_from(&accesspass_account.data[..]).unwrap();
            assert_eq!(
                accesspass.feed_seats(),
                [FeedSeat {
                    current_users: 0,
                    ..accesspass.feed_seats()[0].clone()
                }],
                "the matching EdgeSeat delete must release the seat"
            );
        }
    }
}
