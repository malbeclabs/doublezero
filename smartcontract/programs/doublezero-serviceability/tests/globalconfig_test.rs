//! Acceptance test for issue #3832: the global-config `device_tunnel_block`
//! must be a private (RFC1918) or link-local (RFC3927) prefix, matching the
//! restriction already enforced on device interface IPs. Anything else is
//! rejected with `DoubleZeroError::InvalidDeviceTunnelBlock` (custom code 88).

use doublezero_serviceability::{
    error::DoubleZeroError,
    instructions::DoubleZeroInstruction,
    pda::*,
    processors::{
        globalconfig::set::SetGlobalConfigArgs, permission::create::PermissionCreateArgs,
    },
    resource::ResourceType,
    state::permission::permission_flags,
};
use solana_program::program_error::ProgramError;
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, InstructionError},
    signature::{Keypair, Signer},
    transaction::TransactionError,
};

mod test_helpers;
use test_helpers::*;

/// Initialize global state and attempt to set the global config with the given
/// `device_tunnel_block` (all other blocks are valid). Returns the transaction
/// result so callers can assert success or a specific failure.
async fn set_global_config_with_device_block(
    device_tunnel_block: &str,
) -> Result<(), BanksClientError> {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

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

    execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
            local_asn: 65000,
            remote_asn: 65001,
            device_tunnel_block: device_tunnel_block.parse().unwrap(),
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
    .await
}

#[tokio::test]
async fn test_set_globalconfig_rejects_public_device_tunnel_block() {
    let err = set_global_config_with_device_block("8.8.8.0/24")
        .await
        .expect_err("expected a public device_tunnel_block to be rejected");

    let expected: ProgramError = DoubleZeroError::InvalidDeviceTunnelBlock.into();
    let ProgramError::Custom(expected_code) = expected else {
        panic!("InvalidDeviceTunnelBlock must map to ProgramError::Custom");
    };

    match err {
        BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(code),
        )) => assert_eq!(
            code, expected_code,
            "expected InvalidDeviceTunnelBlock (Custom({expected_code})), got Custom({code})"
        ),
        other => panic!("expected Custom({expected_code}) InstructionError, got {other:?}"),
    }
}

#[tokio::test]
async fn test_set_globalconfig_accepts_private_device_tunnel_block() {
    let result = set_global_config_with_device_block("10.100.0.0/24").await;
    assert!(
        result.is_ok(),
        "private device_tunnel_block should be accepted: {result:?}"
    );
}

#[tokio::test]
async fn test_set_globalconfig_accepts_link_local_device_tunnel_block() {
    let result = set_global_config_with_device_block("169.254.0.0/24").await;
    assert!(
        result.is_ok(),
        "link-local device_tunnel_block should be accepted: {result:?}"
    );
}

/// A non-foundation key holding a GLOBALSTATE_ADMIN Permission account can set the
/// global config — exercises the new Permission-account path on the long
/// (10-account) instruction layout.
#[tokio::test]
async fn test_set_globalconfig_with_permission_account_allowed() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

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

    // A keypair that is NOT in the foundation allowlist.
    let gs_admin = Keypair::new();
    transfer(&mut banks_client, &payer, &gs_admin.pubkey(), 1_000_000_000).await;

    // Foundation grants it a Permission account with GLOBALSTATE_ADMIN.
    let (permission_pda, _) = get_permission_pda(&program_id, &gs_admin.pubkey());
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreatePermission(PermissionCreateArgs {
            user_payer: gs_admin.pubkey(),
            permissions: permission_flags::GLOBALSTATE_ADMIN,
        }),
        vec![
            AccountMeta::new(permission_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

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

    // The GLOBALSTATE_ADMIN holder sets the global config, passing its Permission
    // PDA as the optional trailing account authorize() reads.
    let recent_blockhash = wait_for_new_blockhash(&mut banks_client).await;
    let mut tx = create_transaction_with_extra_accounts(
        program_id,
        &DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
            local_asn: 65000,
            remote_asn: 65001,
            device_tunnel_block: "10.100.0.0/24".parse().unwrap(),
            user_tunnel_block: "169.254.0.0/24".parse().unwrap(),
            multicastgroup_block: "239.0.0.0/24".parse().unwrap(),
            multicast_publisher_block: "148.51.120.0/21".parse().unwrap(),
            next_bgp_community: None,
        }),
        &vec![
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
        &gs_admin,
        &[AccountMeta::new_readonly(permission_pda, false)],
    );
    tx.try_sign(&[&gs_admin], recent_blockhash).unwrap();
    banks_client
        .process_transaction(tx)
        .await
        .expect("GLOBALSTATE_ADMIN permission holder should be able to set global config");

    println!("✅ SetGlobalConfig with GLOBALSTATE_ADMIN permission succeeded");
}
