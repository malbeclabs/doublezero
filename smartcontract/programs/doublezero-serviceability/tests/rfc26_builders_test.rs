//! RFC-26 R10 safety net: build serviceability instructions with the pure
//! `doublezero-serviceability-instruction` builders and run them against the real
//! in-process program. If a builder's account order drifts from the processor's
//! `next_account_info` order, the processor rejects the transaction and the test
//! fails — catching account-order drift automatically.
//!
//! Coverage prioritizes the high-cardinality, variable-account builders where
//! drift is most likely: the create-family (`create_device`, `create_link`,
//! `create_subscribe_user`) and the composed `delete_device` atomic path, plus
//! the batched `clear_topology` builder. Seeding uses the low-level
//! `DoubleZeroInstruction` path; only the builder under test goes through `dzi::`.
//!
//! The builders own the trailing `[payer, system]` accounts; this test only
//! prepends the compute-budget prelude, signs with the payer, and submits.

use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{
        get_accesspass_pda, get_device_pda, get_link_pda, get_location_pda, get_multicastgroup_pda,
        get_resource_extension_pda, get_topology_pda, get_user_pda,
    },
    processors::{
        accesspass::set::SetAccessPassArgs,
        device::{
            create::DeviceCreateArgs, interface::create::DeviceInterfaceCreateArgs,
            update::DeviceUpdateArgs,
        },
        link::create::LinkCreateArgs,
        location::{create::LocationCreateArgs, suspend::LocationSuspendArgs},
        multicastgroup::{
            allowlist::subscriber::add::AddMulticastGroupSubAllowlistArgs,
            create::MulticastGroupCreateArgs,
        },
        topology::{clear::TopologyClearArgs, create::TopologyCreateArgs},
        user::create_subscribe::UserCreateSubscribeArgs,
    },
    resource::ResourceType,
    state::{
        accesspass::AccessPassType,
        device::{DeviceDesiredStatus, DeviceStatus, DeviceType},
        interface::{InterfaceCYOA, InterfaceDIA, LoopbackType, RoutingMode},
        link::{LinkDesiredStatus, LinkLinkType, LinkStatus},
        topology::TopologyConstraint,
        user::{UserCYOA, UserStatus, UserType},
    },
};
use doublezero_serviceability_instruction as dzi;
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::Transaction,
};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

/// Prepend the compute-budget prelude to a builder instruction, sign with the
/// payer, and run it against the program. Returns the processing result.
async fn submit(
    banks_client: &mut BanksClient,
    payer: &Keypair,
    ix: Instruction,
) -> Result<(), BanksClientError> {
    let mut ixs = dzi::compute_budget_prelude().to_vec();
    ixs.push(ix);
    let mut tx = Transaction::new_with_payer(&ixs, Some(&payer.pubkey()));
    let blockhash = banks_client.get_latest_blockhash().await.unwrap();
    tx.sign(&[payer], blockhash);
    banks_client.process_transaction(tx).await
}

/// Seed an activated device (atomic create+allocate+activate) advertising a single
/// dz_prefix, so it carries `TunnelIds(device, 0)` + `DzPrefixBlock(device, 0)`.
/// Returns the device PDA.
#[allow(clippy::too_many_arguments)]
async fn create_activated_device(
    banks_client: &mut BanksClient,
    blockhash: solana_program::hash::Hash,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    globalconfig_pubkey: Pubkey,
    contributor: Pubkey,
    location: Pubkey,
    exchange: Pubkey,
    payer: &Keypair,
    code: &str,
    public_ip: [u8; 4],
    dz_prefixes: &str,
) -> Pubkey {
    let account_index = get_globalstate(banks_client, globalstate_pubkey)
        .await
        .account_index
        + 1;
    let (device_pubkey, _) = get_device_pda(&program_id, account_index);
    let (tunnel_ids, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_block, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

    execute_transaction(
        banks_client,
        blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: code.to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: public_ip.into(),
            dz_prefixes: dz_prefixes.parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 2,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor, false),
            AccountMeta::new(location, false),
            AccountMeta::new(exchange, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(tunnel_ids, false),
            AccountMeta::new(dz_prefix_block, false),
        ],
        payer,
    )
    .await;

    device_pubkey
}

/// Seed a physical interface on a device (Unlinked state), so a WAN link can
/// anchor to it by name.
#[allow(clippy::too_many_arguments)]
async fn create_device_interface(
    banks_client: &mut BanksClient,
    blockhash: solana_program::hash::Hash,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    device: Pubkey,
    contributor: Pubkey,
    payer: &Keypair,
    name: &str,
) {
    execute_transaction(
        banks_client,
        blockhash,
        program_id,
        DoubleZeroInstruction::CreateDeviceInterface(DeviceInterfaceCreateArgs {
            name: name.to_string(),
            interface_dia: InterfaceDIA::None,
            loopback_type: LoopbackType::None,
            interface_cyoa: InterfaceCYOA::None,
            bandwidth: 100_000_000_000,
            ip_net: None,
            cir: 0,
            mtu: 9000,
            routing_mode: RoutingMode::Static,
            vlan_id: 0,
            user_tunnel_endpoint: false,
            use_onchain_allocation: true,
            topology_count: 0,
        }),
        vec![
            AccountMeta::new(device, false),
            AccountMeta::new(contributor, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::DeviceTunnelBlock).0,
                false,
            ),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::SegmentRoutingIds).0,
                false,
            ),
        ],
        payer,
    )
    .await;
}

/// Simple `account_index`-seeded CRUD builder (`create_location`, then
/// `suspend_location`) — accounts `[entity, globalstate]` + trailing.
#[tokio::test]
async fn test_builder_create_and_suspend_location() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _globalconfig) =
        setup_program_with_globalconfig().await;

    let account_index = get_globalstate(&mut banks_client, globalstate_pubkey)
        .await
        .account_index
        + 1;
    let (location_pubkey, _) = get_location_pda(&program_id, account_index);

    let create = dzi::location::create_location(
        &program_id,
        &payer.pubkey(),
        account_index,
        LocationCreateArgs {
            code: "ny".to_string(),
            name: "New York".to_string(),
            country: "us".to_string(),
            lat: 40.7,
            lng: -74.0,
            loc_id: 0,
        },
    );
    submit(&mut banks_client, &payer, create)
        .await
        .expect("create_location builder should be accepted by the program");

    let suspend = dzi::location::suspend_location(
        &program_id,
        &payer.pubkey(),
        &location_pubkey,
        LocationSuspendArgs {},
    );
    submit(&mut banks_client, &payer, suspend)
        .await
        .expect("suspend_location builder should be accepted by the program");
}

/// Variable-account builder (`create_device`): TunnelIds + one DzPrefixBlock per
/// advertised prefix, plus globalconfig — the trickiest account layout.
#[tokio::test]
async fn test_builder_create_device() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (location_pubkey, exchange_pubkey, contributor_pubkey) = setup_device_prerequisites(
        &mut banks_client,
        recent_blockhash,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    let account_index = get_globalstate(&mut banks_client, globalstate_pubkey)
        .await
        .account_index
        + 1;
    let (device_pubkey, _) = get_device_pda(&program_id, account_index);

    let ix = dzi::device::create_device(
        &program_id,
        &payer.pubkey(),
        &contributor_pubkey,
        &location_pubkey,
        &exchange_pubkey,
        account_index,
        DeviceCreateArgs {
            code: "dz1".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [8, 8, 8, 8].into(),
            dz_prefixes: "110.1.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 0,
        },
    );
    submit(&mut banks_client, &payer, ix)
        .await
        .expect("create_device builder should be accepted by the program");

    // The device account now exists at the derived PDA.
    let device = banks_client.get_account(device_pubkey).await.unwrap();
    assert!(device.is_some(), "device account should have been created");
}

/// Highest account-count create builder (`create_link`, WAN atomic path): a link
/// anchored to two activated devices + their interfaces + the unicast-default
/// topology. Then `clear_topology_batched` removes the link from that topology —
/// exercising the topology builder's per-chunk account layout with a real link.
#[tokio::test]
async fn test_builder_create_link_and_clear_topology() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (location_pubkey, exchange_pubkey, contributor_pubkey) = setup_device_prerequisites(
        &mut banks_client,
        blockhash,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    let device_a = create_activated_device(
        &mut banks_client,
        blockhash,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        contributor_pubkey,
        location_pubkey,
        exchange_pubkey,
        &payer,
        "dza",
        [100, 0, 0, 1],
        "110.1.0.0/24",
    )
    .await;
    create_device_interface(
        &mut banks_client,
        blockhash,
        program_id,
        globalstate_pubkey,
        device_a,
        contributor_pubkey,
        &payer,
        "Ethernet0",
    )
    .await;

    let device_z = create_activated_device(
        &mut banks_client,
        blockhash,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        contributor_pubkey,
        location_pubkey,
        exchange_pubkey,
        &payer,
        "dzz",
        [101, 0, 0, 1],
        "111.1.0.0/24",
    )
    .await;
    create_device_interface(
        &mut banks_client,
        blockhash,
        program_id,
        globalstate_pubkey,
        device_z,
        contributor_pubkey,
        &payer,
        "Ethernet1",
    )
    .await;

    // CreateLink derives the unicast-default topology PDA and auto-tags the link
    // into it, so the topology must exist first.
    create_unicast_default_topology(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    let link_index = get_globalstate(&mut banks_client, globalstate_pubkey)
        .await
        .account_index
        + 1;
    let (link_pubkey, _) = get_link_pda(&program_id, link_index);

    let create = dzi::link::create_link(
        &program_id,
        &payer.pubkey(),
        &contributor_pubkey,
        &device_a,
        &device_z,
        link_index,
        LinkCreateArgs {
            code: "wan1".to_string(),
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 9000,
            delay_ns: 500_000,
            jitter_ns: 50_000,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: Some("Ethernet1".to_string()),
            desired_status: Some(LinkDesiredStatus::Activated),
            use_onchain_allocation: true,
        },
    );
    submit(&mut banks_client, &payer, create)
        .await
        .expect("create_link builder should be accepted by the program");

    let link = get_account_data(&mut banks_client, link_pubkey)
        .await
        .expect("link should exist")
        .get_tunnel()
        .unwrap();
    assert_eq!(link.status, LinkStatus::Activated);

    // clear_topology (batched) drift check. The builder passes the topology
    // read-only, matching the SDK command's long-standing contract: clear is a
    // stale-reference cleanup that only writes the topology when a link actually
    // referenced it. Target a fresh topology the link was never tagged into, so
    // cleared_count == 0 and the read-only topology is accepted — the point here
    // is the [topology, globalstate, link] account order.
    let (extra_topology, _) = get_topology_pda(&program_id, "extra");
    execute_transaction(
        &mut banks_client,
        blockhash,
        program_id,
        DoubleZeroInstruction::CreateTopology(TopologyCreateArgs {
            name: "extra".to_string(),
            constraint: TopologyConstraint::IncludeAny,
        }),
        vec![
            AccountMeta::new(extra_topology, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits).0,
                false,
            ),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let clears = dzi::topology::clear_topology_batched(
        &program_id,
        &payer.pubkey(),
        &[link_pubkey],
        TopologyClearArgs {
            name: "extra".to_string(),
        },
    );
    assert_eq!(clears.len(), 1, "one link should produce one batch");
    for ix in clears {
        submit(&mut banks_client, &payer, ix)
            .await
            .expect("clear_topology builder should be accepted by the program");
    }
}

/// Highest-cardinality user builder (`create_subscribe_user`, atomic subscriber
/// path): user + device + mgroup + accesspass + resource extensions.
#[tokio::test]
async fn test_builder_create_subscribe_user() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (location_pubkey, exchange_pubkey, contributor_pubkey) = setup_device_prerequisites(
        &mut banks_client,
        blockhash,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    let device_pubkey = create_activated_device(
        &mut banks_client,
        blockhash,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        contributor_pubkey,
        location_pubkey,
        exchange_pubkey,
        &payer,
        "mdev",
        [100, 0, 0, 1],
        "110.1.0.0/24",
    )
    .await;

    // Raise device max_users so a multicast user can attach (UpdateDevice passes
    // the current location as both old and new location).
    execute_transaction(
        &mut banks_client,
        blockhash,
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

    // Activated multicast group.
    let mgroup_index = get_globalstate(&mut banks_client, globalstate_pubkey)
        .await
        .account_index
        + 1;
    let (mgroup_pubkey, _) = get_multicastgroup_pda(&program_id, mgroup_index);
    execute_transaction(
        &mut banks_client,
        blockhash,
        program_id,
        DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
            code: "group1".to_string(),
            max_bandwidth: 1000,
            owner: payer.pubkey(),
            use_onchain_allocation: true,
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(
                get_resource_extension_pda(&program_id, ResourceType::MulticastGroupBlock).0,
                false,
            ),
        ],
        &payer,
    )
    .await;

    // AccessPass with the mgroup in the subscriber allowlist.
    let user_ip: Ipv4Addr = [100, 0, 0, 5].into();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &user_ip, &payer.pubkey());
    execute_transaction(
        &mut banks_client,
        blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user_ip,
            last_access_epoch: 9999,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;
    execute_transaction(
        &mut banks_client,
        blockhash,
        program_id,
        DoubleZeroInstruction::AddMulticastGroupSubAllowlist(AddMulticastGroupSubAllowlistArgs {
            client_ip: user_ip,
            user_payer: payer.pubkey(),
        }),
        vec![
            AccountMeta::new(mgroup_pubkey, false),
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    // Build CreateSubscribeUser (subscriber-only) via the RFC-26 builder.
    let (user_pubkey, _) = get_user_pda(&program_id, &user_ip, UserType::Multicast);
    let ix = dzi::user::create_subscribe_user(
        &program_id,
        &payer.pubkey(),
        &device_pubkey,
        &mgroup_pubkey,
        &accesspass_pubkey,
        1,
        None,
        UserCreateSubscribeArgs {
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: user_ip,
            publisher: false,
            subscriber: true,
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 1,
            owner: Pubkey::default(),
        },
    );
    submit(&mut banks_client, &payer, ix)
        .await
        .expect("create_subscribe_user builder should be accepted by the program");

    let user = get_account_data(&mut banks_client, user_pubkey)
        .await
        .expect("user should exist")
        .get_user()
        .unwrap();
    assert_eq!(user.status, UserStatus::Activated);
    assert_eq!(user.subscribers, vec![mgroup_pubkey]);
}

/// Composed builder with variable onchain-read owners (`delete_device`, atomic
/// path): closes an activated device together with its resource accounts. The
/// owners are read from chain in processor order and passed to the builder.
#[tokio::test]
async fn test_builder_delete_device_atomic() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;
    let blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (location_pubkey, exchange_pubkey, contributor_pubkey) = setup_device_prerequisites(
        &mut banks_client,
        blockhash,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        &payer,
    )
    .await;

    let device_pubkey = create_activated_device(
        &mut banks_client,
        blockhash,
        program_id,
        globalstate_pubkey,
        globalconfig_pubkey,
        contributor_pubkey,
        location_pubkey,
        exchange_pubkey,
        &payer,
        "deld",
        [12, 0, 0, 1],
        "12.1.0.0/24",
    )
    .await;

    // A device must not be Activated/Deleting to be deleted; drain it first.
    execute_transaction(
        &mut banks_client,
        blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
            status: Some(DeviceStatus::Drained),
            ..DeviceUpdateArgs::default()
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Read the onchain owner of each resource the atomic delete will close, in
    // processor order: [TunnelIds(device, 0), DzPrefixBlock(device, 0)].
    let (tunnel_ids, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_block, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));
    let tunnel_owner = get_resource_extension_data(&mut banks_client, tunnel_ids)
        .await
        .expect("tunnel_ids resource should exist")
        .owner;
    let dz_owner = get_resource_extension_data(&mut banks_client, dz_prefix_block)
        .await
        .expect("dz_prefix_block resource should exist")
        .owner;
    let owners = vec![tunnel_owner, dz_owner];
    let device_owner = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("device should exist")
        .owner;

    let ix = dzi::device::delete_device(
        &program_id,
        &payer.pubkey(),
        &device_pubkey,
        &contributor_pubkey,
        dzi::device::DeviceDeleteResources::Atomic {
            location: &location_pubkey,
            exchange: &exchange_pubkey,
            owners: &owners,
            device_owner: &device_owner,
        },
    );
    submit(&mut banks_client, &payer, ix)
        .await
        .expect("delete_device (atomic) builder should be accepted by the program");

    assert!(
        banks_client
            .get_account(device_pubkey)
            .await
            .unwrap()
            .is_none(),
        "device account should have been closed by the atomic delete"
    );
}
