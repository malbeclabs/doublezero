//! Tests for TopologyInfo, FlexAlgoNodeSegment, and InterfaceV3 (RFC-18 / Link Classification).

use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::get_resource_extension_pda,
    processors::resource::create::ResourceCreateArgs,
    resource::{IdOrIp, ResourceType},
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_admin_group_bits_create_and_pre_mark() {
    println!("[TEST] test_admin_group_bits_create_and_pre_mark");

    let (mut banks_client, payer, program_id, globalstate_pubkey, globalconfig_pubkey) =
        setup_program_with_globalconfig().await;

    let recent_blockhash = banks_client.get_latest_blockhash().await.unwrap();

    let (resource_pubkey, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::AdminGroupBits);

    // Create the AdminGroupBits resource extension
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateResource(ResourceCreateArgs {
            resource_type: ResourceType::AdminGroupBits,
        }),
        vec![
            AccountMeta::new(resource_pubkey, false),
            AccountMeta::new(Pubkey::default(), false), // associated_account (not used)
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Verify the account was created and has data
    let account = banks_client
        .get_account(resource_pubkey)
        .await
        .unwrap()
        .expect("AdminGroupBits account should exist");

    assert!(
        !account.data.is_empty(),
        "AdminGroupBits account should have non-empty data"
    );

    // Verify bit 1 (UNICAST-DRAINED) is pre-marked
    let resource = get_resource_extension_data(&mut banks_client, resource_pubkey)
        .await
        .expect("AdminGroupBits resource extension should be deserializable");

    let allocated = resource.iter_allocated();
    assert_eq!(allocated.len(), 1, "exactly one bit should be pre-marked");
    assert_eq!(
        allocated[0],
        IdOrIp::Id(1),
        "bit 1 (UNICAST-DRAINED) should be pre-marked"
    );

    println!("[PASS] test_admin_group_bits_create_and_pre_mark");
}

#[test]
fn test_topology_info_roundtrip() {
    use doublezero_serviceability::state::{
        accounttype::AccountType,
        topology::{TopologyConstraint, TopologyInfo},
    };

    let info = TopologyInfo {
        account_type: AccountType::Topology,
        owner: solana_sdk::pubkey::Pubkey::new_unique(),
        bump_seed: 42,
        name: "unicast-default".to_string(),
        admin_group_bit: 0,
        flex_algo_number: 128,
        constraint: TopologyConstraint::IncludeAny,
    };
    let bytes = borsh::to_vec(&info).unwrap();
    let decoded = TopologyInfo::try_from(bytes.as_slice()).unwrap();
    assert_eq!(decoded, info);
}

#[test]
fn test_flex_algo_node_segment_roundtrip() {
    use doublezero_serviceability::state::topology::FlexAlgoNodeSegment;

    let seg = FlexAlgoNodeSegment {
        topology: solana_sdk::pubkey::Pubkey::new_unique(),
        node_segment_idx: 1001,
    };
    let bytes = borsh::to_vec(&seg).unwrap();
    let decoded: FlexAlgoNodeSegment = borsh::from_slice(&bytes).unwrap();
    assert_eq!(decoded.node_segment_idx, 1001);
}

#[test]
fn test_interface_v3_defaults_flex_algo_node_segments_empty() {
    use doublezero_serviceability::state::interface::InterfaceV3;
    let iface = InterfaceV3::default();
    assert!(iface.flex_algo_node_segments.is_empty());
}
