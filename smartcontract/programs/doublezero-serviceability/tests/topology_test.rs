//! Integration tests for AdminGroupBits ResourceExtension (RFC-18 / Link Classification).

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
