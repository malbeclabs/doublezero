//! A payer's Permission PDA that has never been created (or was closed) is treated by
//! `authorize()` exactly like no Permission account at all, so clients can append it
//! unconditionally and offline. These tests drive the real program through an
//! `authorize()`-gated instruction with that uninitialized PDA as the trailing account.

use doublezero_serviceability::{
    error::DoubleZeroError,
    instructions::*,
    pda::get_permission_pda,
    processors::{
        allowlist::qa::add::AddQaAllowlistArgs,
        device::{create::DeviceCreateArgs, interface::create::DeviceInterfaceCreateArgs},
        globalstate::setfeatureflags::SetFeatureFlagsArgs,
        link::{create::LinkCreateArgs, update::LinkUpdateArgs},
        permission::create::PermissionCreateArgs,
        topology::{clear::TopologyClearArgs, create::TopologyCreateArgs},
    },
    state::{
        device::{DeviceDesiredStatus, DeviceType},
        feature_flags::FeatureFlag,
        interface::{InterfaceCYOA, InterfaceDIA, LoopbackType, RoutingMode},
        link::{LinkDesiredStatus, LinkLinkType},
        permission::permission_flags,
        topology::TopologyConstraint,
    },
};
use doublezero_serviceability_instruction::{self as dzi, link::LinkUpdateAuthority};
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, Instruction, InstructionError},
    pubkey::Pubkey,
    signature::{Keypair, Signer},
    transaction::{Transaction, TransactionError},
};

mod test_helpers;
use test_helpers::*;

/// `AddQaAllowlist` (GLOBALSTATE_ADMIN, legacy: foundation allowlist) with `permission`
/// as the trailing account after `[payer, system]` — the layout the SDK emits.
async fn add_qa_allowlist(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    payer: &Keypair,
    permission: Pubkey,
) -> Result<(), BanksClientError> {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    try_execute_transaction_with_extra_accounts(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddQaAllowlist(AddQaAllowlistArgs {
            pubkey: Pubkey::new_unique(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        payer,
        &[AccountMeta::new_readonly(permission, false)],
    )
    .await
}

async fn enable_strict_mode(
    banks_client: &mut BanksClient,
    program_id: Pubkey,
    globalstate_pubkey: Pubkey,
    payer: &Keypair,
) {
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    execute_transaction(
        banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(SetFeatureFlagsArgs {
            feature_flags: FeatureFlag::RequirePermissionAccounts.to_mask(),
        }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        payer,
    )
    .await;
}

#[tokio::test]
async fn test_uninitialized_permission_pda_strict_off_falls_back_to_legacy() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;
    let (permission_pda, _) = get_permission_pda(&program_id, &payer.pubkey());
    assert!(account_is_closed(&mut banks_client, permission_pda).await);

    // Never-created PDA: the foundation payer is authorized via the legacy allowlist.
    add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        permission_pda,
    )
    .await
    .unwrap();

    // Pre-funded by a third party (still system-owned, no data): same outcome, so a
    // lamport transfer cannot be used to grief the payer into a hard failure.
    transfer(&mut banks_client, &payer, &permission_pda, 1_000_000).await;
    let account = banks_client
        .get_account(permission_pda)
        .await
        .unwrap()
        .unwrap();
    assert_eq!(account.owner, solana_system_interface::program::ID);
    assert!(account.data.is_empty());
    add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        permission_pda,
    )
    .await
    .unwrap();

    // A key with no legacy authority is still denied.
    let outsider = Keypair::new();
    transfer(&mut banks_client, &payer, &outsider.pubkey(), 1_000_000_000).await;
    let (outsider_pda, _) = get_permission_pda(&program_id, &outsider.pubkey());
    let err = add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &outsider,
        outsider_pda,
    )
    .await
    .unwrap_err();
    assert_custom_error(&err, DoubleZeroError::NotAllowed);
}

#[tokio::test]
async fn test_uninitialized_permission_pda_wrong_pda_rejected() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;
    // Someone else's (uninitialized) Permission PDA is not the payer's.
    let (other_pda, _) = get_permission_pda(&program_id, &Pubkey::new_unique());

    let err = add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        other_pda,
    )
    .await
    .unwrap_err();
    assert!(
        matches!(
            err,
            BanksClientError::TransactionError(TransactionError::InstructionError(
                0,
                InstructionError::InvalidArgument
            ))
        ),
        "expected InvalidArgument, got {err:?}"
    );
}

#[tokio::test]
async fn test_uninitialized_permission_pda_strict_on() {
    let (mut banks_client, payer, program_id, globalstate_pubkey, _) =
        setup_program_with_globalconfig().await;
    let (permission_pda, _) = get_permission_pda(&program_id, &payer.pubkey());

    enable_strict_mode(&mut banks_client, program_id, globalstate_pubkey, &payer).await;

    // Strict mode: the legacy allowlist no longer authorizes, with or without lamports.
    let err = add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        permission_pda,
    )
    .await
    .unwrap_err();
    assert_custom_error(&err, DoubleZeroError::NotAllowed);

    transfer(&mut banks_client, &payer, &permission_pda, 1_000_000).await;
    let err = add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        permission_pda,
    )
    .await
    .unwrap_err();
    assert_custom_error(&err, DoubleZeroError::NotAllowed);

    // ...except the foundation PERMISSION_ADMIN recovery: a foundation member can still
    // create Permission accounts — here its own, whose PDA is both the account being
    // created and the (still uninitialized, pre-funded) trailing Permission account.
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    try_execute_transaction_with_extra_accounts(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: payer.pubkey(),
            permissions: permission_flags::GLOBALSTATE_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
        &[AccountMeta::new_readonly(permission_pda, false)],
    )
    .await
    .unwrap();

    // With the Permission account now initialized, the same instruction succeeds.
    add_qa_allowlist(
        &mut banks_client,
        program_id,
        globalstate_pubkey,
        &payer,
        permission_pda,
    )
    .await
    .unwrap();
}

// ── Variable-length leading account list ─────────────────────────────────────
//
// `ClearTopology` takes `[topology, globalstate, link..]` ahead of `[payer, system]`, and
// `split_trailing_permission` peels the trailing Permission PDA off by address alone. These
// tests send that layout as the instruction builder emits it — links, then payer, system and
// the payer's uninitialized PDA — and check every link is read as a link.

const TOPOLOGY: &str = "topo-a";
const LINK_COUNT: usize = 3;

async fn submit(
    banks_client: &mut BanksClient,
    payer: &Keypair,
    ix: Instruction,
) -> Result<(), BanksClientError> {
    let mut all = dzi::compute_budget_prelude().to_vec();
    all.push(ix);
    let mut tx = Transaction::new_with_payer(&all, Some(&payer.pubkey()));
    let blockhash = wait_for_new_blockhash(banks_client).await;
    tx.sign(&[payer], blockhash);
    banks_client.process_transaction(tx).await
}

async fn next_account_index(banks_client: &mut BanksClient, globalstate: Pubkey) -> u128 {
    get_globalstate(banks_client, globalstate)
        .await
        .account_index
        + 1
}

/// The network a `ClearTopology` walks, built through the program's own instructions.
struct TopologyFixture {
    banks_client: BanksClient,
    payer: Keypair,
    program_id: Pubkey,
    globalstate: Pubkey,
    contributor: Pubkey,
    topology: Pubkey,
    links: Vec<Pubkey>,
}

/// Creates `TOPOLOGY` and `LINK_COUNT` activated WAN links between two activated devices,
/// all signed by the foundation payer. The links are not yet tagged; see [`tag_links`].
async fn setup_topology() -> TopologyFixture {
    let (mut banks_client, payer, program_id, globalstate, globalconfig) =
        setup_program_with_globalconfig().await;
    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();
    let (location, exchange, contributor) = setup_device_prerequisites(
        &mut banks_client,
        recent_blockhash,
        program_id,
        globalstate,
        globalconfig,
        &payer,
    )
    .await;
    // Link activation tags every new link with unicast-default, so it must exist.
    create_unicast_default_topology(
        &mut banks_client,
        program_id,
        globalstate,
        globalconfig,
        &payer,
    )
    .await;

    let ix = dzi::topology::create_topology(
        &program_id,
        &payer.pubkey(),
        TopologyCreateArgs {
            name: TOPOLOGY.to_string(),
            constraint: TopologyConstraint::IncludeAny,
        },
    );
    let topology = ix.accounts[0].pubkey;
    submit(&mut banks_client, &payer, ix).await.unwrap();

    let mut devices = Vec::new();
    for (code, public_ip, dz_prefixes) in [
        ("dza", [8, 8, 8, 8], "110.1.0.0/23"),
        ("dzb", [9, 9, 9, 9], "111.1.0.0/23"),
    ] {
        let index = next_account_index(&mut banks_client, globalstate).await;
        let ix = dzi::device::create_device(
            &program_id,
            &payer.pubkey(),
            &contributor,
            &location,
            &exchange,
            index,
            DeviceCreateArgs {
                code: code.to_string(),
                device_type: DeviceType::Hybrid,
                public_ip: public_ip.into(),
                dz_prefixes: dz_prefixes.parse().unwrap(),
                metrics_publisher_pk: Pubkey::default(),
                mgmt_vrf: "mgmt".to_string(),
                desired_status: Some(DeviceDesiredStatus::Activated),
                resource_count: 0, // derived by the builder
            },
        );
        devices.push(ix.accounts[0].pubkey);
        submit(&mut banks_client, &payer, ix).await.unwrap();
    }
    let (side_a, side_z) = (devices[0], devices[1]);

    let mut links = Vec::new();
    for i in 0..LINK_COUNT {
        let iface = format!("Ethernet{i}");
        for device in [side_a, side_z] {
            let ix = dzi::device::create_device_interface(
                &program_id,
                &payer.pubkey(),
                &device,
                &contributor,
                &[],
                DeviceInterfaceCreateArgs {
                    name: iface.clone(),
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
                },
            );
            submit(&mut banks_client, &payer, ix).await.unwrap();
        }

        let index = next_account_index(&mut banks_client, globalstate).await;
        let ix = dzi::link::create_link(
            &program_id,
            &payer.pubkey(),
            &contributor,
            &side_a,
            &side_z,
            index,
            LinkCreateArgs {
                code: format!("dza-dzb-{i}"),
                link_type: LinkLinkType::WAN,
                bandwidth: 20_000_000_000,
                mtu: 9000,
                delay_ns: 1_000_000,
                jitter_ns: 100_000,
                side_a_iface_name: iface.clone(),
                side_z_iface_name: Some(iface),
                desired_status: Some(LinkDesiredStatus::Activated),
                use_onchain_allocation: true,
            },
        );
        links.push(ix.accounts[0].pubkey);
        submit(&mut banks_client, &payer, ix).await.unwrap();
    }

    TopologyFixture {
        banks_client,
        payer,
        program_id,
        globalstate,
        contributor,
        topology,
        links,
    }
}

/// Tags every link with `TOPOLOGY` (and only it) via `UpdateLink`, which also raises the
/// topology's `reference_count` once per link.
async fn tag_links(fx: &mut TopologyFixture) {
    for link in fx.links.clone() {
        let mut union = link_topologies(&mut fx.banks_client, link).await;
        if !union.contains(&fx.topology) {
            union.push(fx.topology);
        }
        let ix = dzi::link::update_link(
            &fx.program_id,
            &fx.payer.pubkey(),
            &link,
            LinkUpdateAuthority::Contributor {
                contributor: &fx.contributor,
            },
            &Pubkey::default(),
            &Pubkey::default(),
            &union,
            LinkUpdateArgs {
                link_topologies: Some(vec![fx.topology]),
                ..Default::default()
            },
        );
        submit(&mut fx.banks_client, &fx.payer, ix).await.unwrap();
    }
}

async fn link_topologies(banks_client: &mut BanksClient, link: Pubkey) -> Vec<Pubkey> {
    get_account_data(banks_client, link)
        .await
        .expect("link")
        .get_tunnel()
        .unwrap()
        .link_topologies
}

async fn topology_reference_count(banks_client: &mut BanksClient, topology: Pubkey) -> u32 {
    get_account_data(banks_client, topology)
        .await
        .expect("topology")
        .get_topology()
        .unwrap()
        .reference_count
}

fn clear_ix(program_id: &Pubkey, payer: &Pubkey, links: &[Pubkey]) -> Instruction {
    let ix = dzi::topology::clear_topology(
        program_id,
        payer,
        links,
        TopologyClearArgs {
            name: TOPOLOGY.to_string(),
        },
    );
    // [topology, globalstate, link.., payer, system, permission]
    assert_eq!(ix.accounts.len(), links.len() + 5);
    assert_eq!(
        ix.accounts.last().unwrap().pubkey,
        get_permission_pda(program_id, payer).0
    );
    ix
}

#[tokio::test]
async fn test_uninitialized_permission_pda_behind_link_list_strict_off() {
    let mut fx = setup_topology().await;
    let (program_id, payer, topology) = (fx.program_id, fx.payer.insecure_clone(), fx.topology);
    let (permission_pda, _) = get_permission_pda(&program_id, &payer.pubkey());

    // Never-created PDA, then pre-funded by a third party (still system-owned, no data):
    // either way the foundation payer is authorized via the legacy allowlist and every
    // link in the list is cleared.
    for prefund in [false, true] {
        if prefund {
            transfer(&mut fx.banks_client, &payer, &permission_pda, 1_000_000).await;
            let account = fx
                .banks_client
                .get_account(permission_pda)
                .await
                .unwrap()
                .unwrap();
            assert_eq!(account.owner, solana_system_interface::program::ID);
            assert!(account.data.is_empty());
        } else {
            assert!(account_is_closed(&mut fx.banks_client, permission_pda).await);
        }

        tag_links(&mut fx).await;
        for link in fx.links.clone() {
            assert_eq!(
                link_topologies(&mut fx.banks_client, link).await,
                vec![topology]
            );
        }
        assert_eq!(
            topology_reference_count(&mut fx.banks_client, topology).await,
            LINK_COUNT as u32
        );

        let ix = clear_ix(&program_id, &payer.pubkey(), &fx.links);
        submit(&mut fx.banks_client, &payer, ix).await.unwrap();

        for link in fx.links.clone() {
            assert!(link_topologies(&mut fx.banks_client, link).await.is_empty());
        }
        assert_eq!(
            topology_reference_count(&mut fx.banks_client, topology).await,
            0
        );
    }
}

#[tokio::test]
async fn test_uninitialized_permission_pda_behind_link_list_strict_on() {
    let mut fx = setup_topology().await;
    tag_links(&mut fx).await;
    let (program_id, payer, topology) = (fx.program_id, fx.payer.insecure_clone(), fx.topology);
    assert_eq!(
        topology_reference_count(&mut fx.banks_client, topology).await,
        LINK_COUNT as u32
    );

    enable_strict_mode(&mut fx.banks_client, program_id, fx.globalstate, &payer).await;

    // A non-foundation signer with an uninitialized PDA, unfunded and then pre-funded.
    let outsider = Keypair::new();
    transfer(
        &mut fx.banks_client,
        &payer,
        &outsider.pubkey(),
        1_000_000_000,
    )
    .await;
    let (outsider_pda, _) = get_permission_pda(&program_id, &outsider.pubkey());
    for prefund in [false, true] {
        if prefund {
            transfer(&mut fx.banks_client, &payer, &outsider_pda, 1_000_000).await;
        }
        let ix = clear_ix(&program_id, &outsider.pubkey(), &fx.links);
        let err = submit(&mut fx.banks_client, &outsider, ix)
            .await
            .unwrap_err();
        assert_custom_error(&err, DoubleZeroError::NotAllowed);
    }

    // Strict mode also withdraws the foundation payer's legacy authority for this flag.
    let ix = clear_ix(&program_id, &payer.pubkey(), &fx.links);
    let err = submit(&mut fx.banks_client, &payer, ix).await.unwrap_err();
    assert_custom_error(&err, DoubleZeroError::NotAllowed);

    for link in fx.links.clone() {
        assert_eq!(
            link_topologies(&mut fx.banks_client, link).await,
            vec![topology]
        );
    }
    assert_eq!(
        topology_reference_count(&mut fx.banks_client, topology).await,
        LINK_COUNT as u32
    );
}
