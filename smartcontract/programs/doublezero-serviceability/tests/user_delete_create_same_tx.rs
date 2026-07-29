//! Atomic delete-then-create of a User account, the shape the V1 → V2 user-PDA migration takes.
//!
//! `get_user_pda` derives the User account from (client_ip, user_type), so a disconnect followed by
//! a reconnect for the same client lands on the *same* address, and `Migrate` moves a legacy V1
//! (index-derived) account onto its V2 address. Both amount to closing one User account and
//! creating another inside a single transaction, which is what these tests exercise:
//!
//! - DeleteUser + CreateUser at the same V2 PDA (reconnect) — succeeds, state fully restored.
//! - DeleteUser at the V1 PDA + CreateUser at the V2 PDA (migration) — succeeds.
//! - The same two instructions in separate transactions (control) — succeeds.
//! - `Migrate` onto a V2 address that is already occupied — fails with `AccountAlreadyInUse`.

use borsh::to_vec;
use doublezero_serviceability::{
    entrypoint::process_instruction,
    instructions::*,
    pda::*,
    processors::{
        accesspass::set::SetAccessPassArgs,
        contributor::create::ContributorCreateArgs,
        device::update::DeviceUpdateArgs,
        user::{create::*, delete::*},
        *,
    },
    resource::ResourceType,
    state::{
        accesspass::{AccessPassStatus, AccessPassType},
        accounttype::AccountType,
        device::*,
        user::{UserCYOA, UserStatus, UserType},
    },
};
use globalconfig::set::SetGlobalConfigArgs;
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
    rent::Rent,
    signature::Keypair,
    signer::Signer,
    transaction::Transaction,
};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

struct TestEnv {
    banks_client: BanksClient,
    payer: Keypair,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    device_pubkey: Pubkey,
}

/// Build a single DoubleZero instruction with the standard trailing payer + system_program metas,
/// so several of them can be packed into one transaction.
fn build_instruction(
    program_id: Pubkey,
    instruction: &DoubleZeroInstruction,
    accounts: Vec<AccountMeta>,
    payer: &Keypair,
) -> Instruction {
    let mut metas = accounts;
    metas.push(AccountMeta::new(payer.pubkey(), true));
    metas.push(AccountMeta::new(
        solana_system_interface::program::ID,
        false,
    ));
    Instruction::new_with_bytes(program_id, &to_vec(instruction).unwrap(), metas)
}

/// Initialize the program environment up to and including an activated device.
async fn setup_test_env() -> TestEnv {
    let program_id = Pubkey::new_unique();
    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    );
    // DeleteUser + CreateUser in one transaction is well past the per-transaction default.
    program_test.set_compute_max_units(1_400_000);
    let (mut banks_client, payer, recent_blockhash) = program_test.start().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let (config_pubkey, _) = get_globalconfig_pda(&program_id);

    let (device_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock);
    let (user_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::UserTunnelBlock);
    let (multicastgroup_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock);
    let (link_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::LinkIds);
    let (segment_routing_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds);
    let (multicast_publisher_block_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::MulticastPublisherBlock);
    let (vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);
    let (admin_group_bits_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
            local_asn: 65000,
            remote_asn: 65001,
            device_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            user_tunnel_block: "169.254.0.0/24".parse().unwrap(),
            multicastgroup_block: "224.0.0.0/16".parse().unwrap(),
            multicast_publisher_block: "148.51.120.0/21".parse().unwrap(),
            next_bgp_community: None,
        }),
        vec![
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(device_tunnel_block_pda, false),
            AccountMeta::new(user_tunnel_block_pda, false),
            AccountMeta::new(multicastgroup_block_pda, false),
            AccountMeta::new(link_ids_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
            AccountMeta::new(multicast_publisher_block_pda, false),
            AccountMeta::new(vrf_ids_pda, false),
            AccountMeta::new(admin_group_bits_pda, false),
        ],
        &payer,
    )
    .await;

    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (location_pubkey, _) = get_location_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(location::create::LocationCreateArgs {
            code: "la".to_string(),
            name: "Los Angeles".to_string(),
            country: "us".to_string(),
            lat: 1.0,
            lng: 2.0,
            loc_id: 0,
        }),
        vec![
            AccountMeta::new(location_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (exchange_pubkey, _) = get_exchange_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(exchange::create::ExchangeCreateArgs {
            code: "la".to_string(),
            name: "Los Angeles".to_string(),
            lat: 1.0,
            lng: 2.0,
            reserved: 0,
        }),
        vec![
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (contributor_pubkey, _) = get_contributor_pda(&program_id, gs.account_index + 1);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "cont".to_string(),
        }),
        vec![
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let gs = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, gs.account_index + 1);
    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(device::create::DeviceCreateArgs {
            code: "la".to_string(),
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
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
        ],
        &payer,
    )
    .await;

    execute_transaction(
        &mut banks_client,
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
        banks_client,
        payer,
        program_id,
        globalstate_pubkey,
        device_pubkey,
    }
}

/// Account metas for CreateUser at the (client_ip, user_type) PDA.
fn create_user_metas(
    env: &TestEnv,
    user_pubkey: Pubkey,
    accesspass_pubkey: Pubkey,
) -> Vec<AccountMeta> {
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

    vec![
        AccountMeta::new(user_pubkey, false),
        AccountMeta::new(env.device_pubkey, false),
        AccountMeta::new(accesspass_pubkey, false),
        AccountMeta::new(env.globalstate_pubkey, false),
        AccountMeta::new(user_tunnel_block_pda, false),
        AccountMeta::new(multicast_publisher_block_pda, false),
        AccountMeta::new(device_tunnel_ids_pda, false),
        AccountMeta::new(dz_prefix_block_pda, false),
    ]
}

/// Account metas for DeleteUser of a single-dz-prefix unicast user.
fn delete_user_metas(
    env: &TestEnv,
    user_pubkey: Pubkey,
    accesspass_pubkey: Pubkey,
    owner: Pubkey,
) -> Vec<AccountMeta> {
    let (user_tunnel_block_pda, _, _) =
        get_resource_extension_pda(&env.program_id, ResourceType::UserTunnelBlock);
    let (device_tunnel_ids_pda, _, _) = get_resource_extension_pda(
        &env.program_id,
        ResourceType::TunnelIds(env.device_pubkey, 0),
    );
    let (dz_prefix_block_pda, _, _) = get_resource_extension_pda(
        &env.program_id,
        ResourceType::DzPrefixBlock(env.device_pubkey, 0),
    );

    vec![
        AccountMeta::new(user_pubkey, false),
        AccountMeta::new(accesspass_pubkey, false),
        AccountMeta::new(env.globalstate_pubkey, false),
        AccountMeta::new(env.device_pubkey, false),
        AccountMeta::new(user_tunnel_block_pda, false),
        AccountMeta::new(device_tunnel_ids_pda, false),
        AccountMeta::new(dz_prefix_block_pda, false),
        AccountMeta::new(owner, false),
    ]
}

/// Provision an access pass for the payer at `accesspass_ip`.
async fn set_access_pass(env: &mut TestEnv, accesspass_ip: Ipv4Addr) -> Pubkey {
    let recent_blockhash = env.banks_client.get_latest_blockhash().await.unwrap();
    let (accesspass_pubkey, _) =
        get_accesspass_pda(&env.program_id, &accesspass_ip, &env.payer.pubkey());

    execute_transaction(
        &mut env.banks_client,
        recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: accesspass_ip,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            max_unicast_users: 2,
            max_multicast_users: 2,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(env.globalstate_pubkey, false),
            AccountMeta::new(env.payer.pubkey(), false),
        ],
        &env.payer,
    )
    .await;

    accesspass_pubkey
}

/// DeleteUser + CreateUser for the *same* (client_ip, user_type) in one transaction: the User PDA
/// is closed and immediately re-created at the same address. This is the migration reconnect shape.
#[tokio::test]
async fn test_delete_and_create_same_user_pda_in_one_transaction() {
    let mut env = setup_test_env().await;
    let user_ip: Ipv4Addr = [100, 0, 0, 1].into();

    let accesspass_pubkey = set_access_pass(&mut env, user_ip).await;
    let (user_pubkey, _) = get_user_pda(&env.program_id, &user_ip, UserType::IBRL);

    // Seed the user that the combined transaction will delete.
    let create_metas = create_user_metas(&env, user_pubkey, accesspass_pubkey);
    let recent_blockhash = env.banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut env.banks_client,
        recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
        }),
        create_metas,
        &env.payer,
    )
    .await;

    let before = get_account_data(&mut env.banks_client, user_pubkey)
        .await
        .expect("user should exist before the combined transaction")
        .get_user()
        .unwrap();
    assert_eq!(before.status, UserStatus::Activated);

    let delete_ix = build_instruction(
        env.program_id,
        &DoubleZeroInstruction::DeleteUser(UserDeleteArgs {
            dz_prefix_count: 1,
            multicast_publisher_count: 0,
        }),
        delete_user_metas(&env, user_pubkey, accesspass_pubkey, before.owner),
        &env.payer,
    );
    let create_ix = build_instruction(
        env.program_id,
        &DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
        }),
        create_user_metas(&env, user_pubkey, accesspass_pubkey),
        &env.payer,
    );

    let recent_blockhash = wait_for_new_blockhash(&mut env.banks_client).await;
    let mut transaction =
        Transaction::new_with_payer(&[delete_ix, create_ix], Some(&env.payer.pubkey()));
    transaction
        .try_sign(&[&env.payer], recent_blockhash)
        .unwrap();

    let result = env.banks_client.process_transaction(transaction).await;
    println!("➡️  DeleteUser + CreateUser (same PDA, one transaction): {result:?}");
    result.expect("close-then-recreate of the same User PDA in one transaction");

    let raw = env
        .banks_client
        .get_account(user_pubkey)
        .await
        .unwrap()
        .expect("user account must exist after the combined transaction");
    println!(
        "user account: owner={} lamports={} data_len={}",
        raw.owner,
        raw.lamports,
        raw.data.len()
    );
    assert_eq!(raw.owner, env.program_id, "user account owner");
    assert!(
        raw.lamports >= Rent::default().minimum_balance(raw.data.len()),
        "re-created user account must be rent exempt: {} lamports for {} bytes",
        raw.lamports,
        raw.data.len()
    );

    // The reconnect must land the user back in exactly the state it was in, reusing the same
    // tunnel id and dz ip that DeleteUser released earlier in the transaction.
    let after = get_account_data(&mut env.banks_client, user_pubkey)
        .await
        .expect("user should be re-created")
        .get_user()
        .unwrap();
    assert_eq!(after.account_type, AccountType::User);
    assert_eq!(after, before);

    // Exactly one connection is accounted for: DeleteUser released the seat and CreateUser
    // re-took it.
    let pass = get_account_data(&mut env.banks_client, accesspass_pubkey)
        .await
        .unwrap()
        .get_accesspass()
        .unwrap();
    assert_eq!(pass.connection_count, 1, "access pass connection_count");
    assert_eq!(pass.status, AccessPassStatus::Connected);

    let device = get_device(&mut env.banks_client, env.device_pubkey)
        .await
        .unwrap();
    assert_eq!(device.users_count, 1, "device users_count");
    assert_eq!(device.unicast_users_count, 1, "device unicast_users_count");
    assert_eq!(device.reference_count, 1, "device reference_count");
}

/// Control for the test above: the same two instructions in *separate* transactions. Isolates any
/// behavior difference to the single-transaction packing rather than to the delete or the re-create
/// on its own.
#[tokio::test]
async fn test_delete_then_create_same_user_pda_in_separate_transactions() {
    let mut env = setup_test_env().await;
    let user_ip: Ipv4Addr = [100, 0, 0, 2].into();

    let accesspass_pubkey = set_access_pass(&mut env, user_ip).await;
    let (user_pubkey, _) = get_user_pda(&env.program_id, &user_ip, UserType::IBRL);

    let create_metas = create_user_metas(&env, user_pubkey, accesspass_pubkey);
    let recent_blockhash = env.banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut env.banks_client,
        recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
        }),
        create_metas.clone(),
        &env.payer,
    )
    .await;

    let before = get_account_data(&mut env.banks_client, user_pubkey)
        .await
        .unwrap()
        .get_user()
        .unwrap();

    let delete_metas = delete_user_metas(&env, user_pubkey, accesspass_pubkey, before.owner);
    let recent_blockhash = wait_for_new_blockhash(&mut env.banks_client).await;
    try_execute_transaction(
        &mut env.banks_client,
        recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::DeleteUser(UserDeleteArgs {
            dz_prefix_count: 1,
            multicast_publisher_count: 0,
        }),
        delete_metas,
        &env.payer,
    )
    .await
    .expect("DeleteUser should succeed on its own");

    assert!(
        get_account_data(&mut env.banks_client, user_pubkey)
            .await
            .is_none(),
        "user account should be closed"
    );

    let recent_blockhash = wait_for_new_blockhash(&mut env.banks_client).await;
    try_execute_transaction(
        &mut env.banks_client,
        recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
        }),
        create_metas,
        &env.payer,
    )
    .await
    .expect("re-creating the user at the same PDA in a later transaction should succeed");

    let after = get_account_data(&mut env.banks_client, user_pubkey)
        .await
        .expect("user should be re-created")
        .get_user()
        .unwrap();
    assert_eq!(after.account_type, AccountType::User);
    assert_eq!(after.client_ip, user_ip);
    assert_eq!(after.status, UserStatus::Activated);
}

/// The migration shape itself: a legacy user still living at the V1 (index) PDA is deleted and the
/// same client is re-created at the V2 (client_ip, user_type) PDA in a single transaction. This is
/// what `Migrate` does inside one instruction, decomposed into the two user instructions.
#[tokio::test]
async fn test_delete_old_pda_and_create_new_pda_user_in_one_transaction() {
    let mut env = setup_test_env().await;
    let user_ip: Ipv4Addr = [100, 0, 0, 3].into();

    let accesspass_pubkey = set_access_pass(&mut env, user_ip).await;

    // Seed a legacy user at the V1 index PDA. CreateUser picks PDAVersion::V1 when the account
    // passed in is the old index-derived address.
    let gs = get_globalstate(&mut env.banks_client, env.globalstate_pubkey).await;
    let (old_user_pubkey, _) = get_user_old_pda(&env.program_id, gs.account_index + 1);
    let old_create_metas = create_user_metas(&env, old_user_pubkey, accesspass_pubkey);

    let recent_blockhash = env.banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut env.banks_client,
        recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
        }),
        old_create_metas,
        &env.payer,
    )
    .await;

    let legacy = get_account_data(&mut env.banks_client, old_user_pubkey)
        .await
        .expect("legacy user should exist at the old PDA")
        .get_user()
        .unwrap();
    assert_ne!(legacy.index, 0, "legacy user should carry a V1 index");

    let (new_user_pubkey, _) = get_user_pda(&env.program_id, &user_ip, UserType::IBRL);
    assert_ne!(old_user_pubkey, new_user_pubkey);

    let delete_ix = build_instruction(
        env.program_id,
        &DoubleZeroInstruction::DeleteUser(UserDeleteArgs {
            dz_prefix_count: 1,
            multicast_publisher_count: 0,
        }),
        delete_user_metas(&env, old_user_pubkey, accesspass_pubkey, legacy.owner),
        &env.payer,
    );
    let create_ix = build_instruction(
        env.program_id,
        &DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
        }),
        create_user_metas(&env, new_user_pubkey, accesspass_pubkey),
        &env.payer,
    );

    let recent_blockhash = wait_for_new_blockhash(&mut env.banks_client).await;
    let mut transaction =
        Transaction::new_with_payer(&[delete_ix, create_ix], Some(&env.payer.pubkey()));
    transaction
        .try_sign(&[&env.payer], recent_blockhash)
        .unwrap();

    let result = env.banks_client.process_transaction(transaction).await;
    println!("➡️  DeleteUser(V1 PDA) + CreateUser(V2 PDA), one transaction: {result:?}");
    result.expect("old-PDA delete plus new-PDA create in one transaction");

    assert!(
        get_account_data(&mut env.banks_client, old_user_pubkey)
            .await
            .is_none(),
        "legacy user account should be closed"
    );

    let migrated = get_account_data(&mut env.banks_client, new_user_pubkey)
        .await
        .expect("user should exist at the new PDA")
        .get_user()
        .unwrap();
    assert_eq!(migrated.account_type, AccountType::User);
    assert_eq!(migrated.client_ip, user_ip);
    assert_eq!(migrated.status, UserStatus::Activated);
    assert_eq!(migrated.index, 0, "V2 users carry no index");
    // The re-created user reuses the tunnel id and dz ip the delete released in the same
    // transaction.
    assert_eq!(migrated.tunnel_id, legacy.tunnel_id);
    assert_eq!(migrated.tunnel_net, legacy.tunnel_net);
    assert_eq!(migrated.dz_ip, legacy.dz_ip);

    let pass = get_account_data(&mut env.banks_client, accesspass_pubkey)
        .await
        .unwrap()
        .get_accesspass()
        .unwrap();
    assert_eq!(pass.connection_count, 1, "access pass connection_count");
    assert_eq!(pass.status, AccessPassStatus::Connected);

    let device = get_device(&mut env.banks_client, env.device_pubkey)
        .await
        .unwrap();
    assert_eq!(device.users_count, 1, "device users_count");
    assert_eq!(device.unicast_users_count, 1, "device unicast_users_count");
    assert_eq!(device.reference_count, 1, "device reference_count");
}

/// `Migrate` cannot move a legacy V1 user onto its V2 address once that address is occupied.
///
/// Nothing stops a client from holding both accounts at once: a Prepaid access pass does not
/// enforce the per-category user cap, so a reconnect that provisions a V2 user while the legacy
/// V1 account is still around leaves two live User accounts for the same (client_ip, user_type).
/// `process_migrate` then calls `try_acc_create` on an address that already has data and lamports,
/// and the system program rejects the allocation with `AccountAlreadyInUse` (`Custom(0)`). Those
/// users are stuck: migration fails for them every time until the duplicate is deleted.
#[tokio::test]
async fn test_migrate_onto_occupied_v2_user_pda_fails() {
    let mut env = setup_test_env().await;
    let user_ip: Ipv4Addr = [100, 0, 0, 4].into();

    let accesspass_pubkey = set_access_pass(&mut env, user_ip).await;

    let gs = get_globalstate(&mut env.banks_client, env.globalstate_pubkey).await;
    let (old_user_pubkey, _) = get_user_old_pda(&env.program_id, gs.account_index + 1);
    let old_create_metas = create_user_metas(&env, old_user_pubkey, accesspass_pubkey);
    let recent_blockhash = env.banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        &mut env.banks_client,
        recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
        }),
        old_create_metas,
        &env.payer,
    )
    .await;

    let (new_user_pubkey, _) = get_user_pda(&env.program_id, &user_ip, UserType::IBRL);
    let new_create_metas = create_user_metas(&env, new_user_pubkey, accesspass_pubkey);
    let recent_blockhash = wait_for_new_blockhash(&mut env.banks_client).await;
    try_execute_transaction(
        &mut env.banks_client,
        recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::CreateUser(UserCreateArgs {
            client_ip: user_ip,
            user_type: UserType::IBRL,
            cyoa_type: UserCYOA::GREOverDIA,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
        }),
        new_create_metas,
        &env.payer,
    )
    .await
    .expect("reconnect creates a second user at the V2 PDA");

    let recent_blockhash = wait_for_new_blockhash(&mut env.banks_client).await;
    let result = try_execute_transaction(
        &mut env.banks_client,
        recent_blockhash,
        env.program_id,
        DoubleZeroInstruction::Migrate(migrate::MigrateArgs {}),
        vec![
            AccountMeta::new(old_user_pubkey, false),
            AccountMeta::new(new_user_pubkey, false),
        ],
        &env.payer,
    )
    .await;
    println!("➡️  Migrate onto occupied V2 PDA: {result:?}");

    let err = result.expect_err("Migrate onto an occupied V2 PDA must fail");
    assert!(
        format!("{err:?}").contains("Custom(0)"),
        "expected system AccountAlreadyInUse (Custom(0)), got: {err:?}"
    );

    // Both accounts survive the failed migration.
    assert!(
        get_account_data(&mut env.banks_client, old_user_pubkey)
            .await
            .is_some(),
        "legacy user must be untouched by the failed migration"
    );
    assert!(
        get_account_data(&mut env.banks_client, new_user_pubkey)
            .await
            .is_some(),
        "V2 user must be untouched by the failed migration"
    );
}
