//! Integration tests for mgmt_vrf validation in CreateDevice / UpdateDevice.

use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::device::{create::DeviceCreateArgs, update::DeviceUpdateArgs},
    resource::ResourceType,
    state::device::*,
};
use solana_program_test::*;
use solana_sdk::{
    instruction::{AccountMeta, InstructionError},
    pubkey::Pubkey,
    transaction::TransactionError,
};

mod test_helpers;
use test_helpers::*;

const INVALID_ACCOUNT_CODE: u32 = 19;
const CODE_TOO_LONG: u32 = 34;

fn assert_custom_error(result: Result<(), BanksClientError>, expected: u32, context: &str) {
    match result {
        Err(BanksClientError::TransactionError(TransactionError::InstructionError(
            0,
            InstructionError::Custom(code),
        ))) if code == expected => {}
        _ => panic!("{context}: expected Custom({expected}), got {result:?}"),
    }
}

#[tokio::test]
async fn test_device_mgmt_vrf_validation() {
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

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (device_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

    let create_args = |mgmt_vrf: &str| {
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "dz1".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [8, 8, 8, 8].into(),
            dz_prefixes: "110.1.0.0/23".parse().unwrap(),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: mgmt_vrf.to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
            resource_count: 2,
        })
    };
    let create_accounts = vec![
        AccountMeta::new(device_pubkey, false),
        AccountMeta::new(contributor_pubkey, false),
        AccountMeta::new(location_pubkey, false),
        AccountMeta::new(exchange_pubkey, false),
        AccountMeta::new(globalstate_pubkey, false),
        AccountMeta::new(globalconfig_pubkey, false),
        AccountMeta::new(tunnel_ids_pda, false),
        AccountMeta::new(dz_prefix_pda, false),
    ];

    // CreateDevice rejects mgmt_vrf values outside the account-code charset.
    for mgmt_vrf in ["mgmt\nbad", "mgmt\r\nbad", "mgmt vrf", "mgmt;vrf"] {
        let result = execute_transaction_expect_failure(
            &mut banks_client,
            recent_blockhash,
            program_id,
            create_args(mgmt_vrf),
            create_accounts.clone(),
            &payer,
        )
        .await;
        assert_custom_error(
            result,
            INVALID_ACCOUNT_CODE,
            &format!("CreateDevice with mgmt_vrf {mgmt_vrf:?}"),
        );
    }

    // CreateDevice rejects an mgmt_vrf longer than the 32-byte cap.
    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        create_args(&"a".repeat(33)),
        create_accounts.clone(),
        &payer,
    )
    .await;
    assert_custom_error(result, CODE_TOO_LONG, "CreateDevice with 33-byte mgmt_vrf");

    // An empty mgmt_vrf (the default VRF) is accepted.
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        create_args(""),
        create_accounts,
        &payer,
    )
    .await;
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    assert_eq!(device.mgmt_vrf, "");

    // UpdateDevice must apply the same validation.
    let update_args = |mgmt_vrf: &str| {
        DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
            mgmt_vrf: Some(mgmt_vrf.to_string()),
            ..DeviceUpdateArgs::default()
        })
    };
    let update_accounts = vec![
        AccountMeta::new(device_pubkey, false),
        AccountMeta::new(contributor_pubkey, false),
        AccountMeta::new(globalstate_pubkey, false),
    ];

    let result = execute_transaction_expect_failure(
        &mut banks_client,
        recent_blockhash,
        program_id,
        update_args("mgmt\nbad"),
        update_accounts.clone(),
        &payer,
    )
    .await;
    assert_custom_error(
        result,
        INVALID_ACCOUNT_CODE,
        "UpdateDevice with newline in mgmt_vrf",
    );

    // The failed update must not have modified the stored value, and a valid
    // update still goes through.
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    assert_eq!(device.mgmt_vrf, "");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        update_args("MGMT-2"),
        update_accounts,
        &payer,
    )
    .await;
    let device = get_device(&mut banks_client, device_pubkey)
        .await
        .expect("Device not found");
    assert_eq!(device.mgmt_vrf, "MGMT-2");
}
