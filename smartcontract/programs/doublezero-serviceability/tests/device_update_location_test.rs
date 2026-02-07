use device::activate::DeviceActivateArgs;
use doublezero_serviceability::{
    entrypoint::*,
    instructions::*,
    pda::*,
    processors::{
        contributor::create::ContributorCreateArgs,
        device::{closeaccount::*, create::*, delete::*, update::*},
        *,
    },
    resource::ResourceType,
    state::{accounttype::AccountType, contributor::ContributorStatus, device::*},
};
use globalconfig::set::SetGlobalConfigArgs;
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signer::Signer};

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn device_update_location_test() {
    let program_id = Pubkey::new_unique();
    let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    )
    .start()
    .await;

    /***********************************************************************************************************************************/
    println!("🟢  Start test_device");
    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    /***********************************************************************************************************************************/
    println!("🟢 1. Global Initialization...");
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

    /***********************************************************************************************************************************/
    println!("🟢 2. Set GlobalConfig...");
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
    let (vrf_ids_pda, _, _) = get_resource_extension_pda(&program_id, ResourceType::VrfIds);
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
            local_asn: 65000,
            remote_asn: 65001,
            device_tunnel_block: "10.0.0.0/24".parse().unwrap(), // Private tunnel block
            user_tunnel_block: "10.0.0.0/24".parse().unwrap(),   // Private tunnel block
            multicastgroup_block: "224.0.0.0/16".parse().unwrap(), // Multicast block
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
            AccountMeta::new(vrf_ids_pda, false),
        ],
        &payer,
    )
    .await;

    /***********************************************************************************************************************************/
    println!("🟢 3. Create Location...");
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 0);

    let (location_la_pubkey, _) =
        get_location_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(location::create::LocationCreateArgs {
            code: "la".to_string(),
            name: "Los Angeles".to_string(),
            country: "us".to_string(),
            lat: 1.234,
            lng: 4.567,
            loc_id: 0,
        }),
        vec![
            AccountMeta::new(location_la_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    /***********************************************************************************************************************************/
    println!("🟢 4. Create Location...");
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 1);

    let (location_ny_pubkey, _) =
        get_location_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(location::create::LocationCreateArgs {
            code: "ny".to_string(),
            name: "New York".to_string(),
            country: "us".to_string(),
            lat: 1.234,
            lng: 4.567,
            loc_id: 0,
        }),
        vec![
            AccountMeta::new(location_ny_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    /***********************************************************************************************************************************/
    println!("🟢 5. Create Exchange...");
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 2);

    let (exchange_pubkey, _) = get_exchange_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(exchange::create::ExchangeCreateArgs {
            code: "la".to_string(),
            name: "Los Angeles".to_string(),
            lat: 1.234,
            lng: 4.567,
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
    /***********************************************************************************************************************************/
    println!("🟢 6. Create Contributor...");
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 3);

    let (contributor_pubkey, _) =
        get_contributor_pda(&program_id, globalstate_account.account_index + 1);

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

    let contributor = get_account_data(&mut banks_client, contributor_pubkey)
        .await
        .expect("Unable to get Account")
        .get_contributor()
        .unwrap();
    assert_eq!(contributor.account_type, AccountType::Contributor);
    assert_eq!(contributor.code, "cont".to_string());
    assert_eq!(contributor.reference_count, 0);
    assert_eq!(contributor.status, ContributorStatus::Activated);

    println!("✅ Contributor initialized successfully",);
    /***********************************************************************************************************************************/
    // Device _la
    println!("🟢 7. Create Device...");
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 4);

    let (device_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);

    let (tunnel_ids_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::TunnelIds(device_pubkey, 0));
    let (dz_prefix_pda, _, _) =
        get_resource_extension_pda(&program_id, ResourceType::DzPrefixBlock(device_pubkey, 0));

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(DeviceCreateArgs {
            code: "la".to_string(),
            device_type: DeviceType::Hybrid,
            public_ip: [8, 8, 8, 8].into(), // Global public IP
            dz_prefixes: "110.1.0.0/23".parse().unwrap(), // Global prefix
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "mgmt".to_string(),
            desired_status: Some(DeviceDesiredStatus::Activated),
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_la_pubkey, false),
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    assert_eq!(device.account_type, AccountType::Device);
    assert_eq!(device.code, "la".to_string());
    assert_eq!(device.status, DeviceStatus::Pending);

    println!("✅ Device Created successfully",);
    /***********************************************************************************************************************************/
    // Device _la
    println!("🟢 8. Update Device ...");

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
            AccountMeta::new(location_la_pubkey, false),
            AccountMeta::new(location_ny_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let device_la = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();
    assert_eq!(device_la.max_users, 128);
    assert_eq!(device_la.location_pk, location_ny_pubkey);

    // check reference counts
    let contributor = get_account_data(&mut banks_client, contributor_pubkey)
        .await
        .expect("Unable to get Account")
        .get_contributor()
        .unwrap();
    assert_eq!(contributor.reference_count, 1);
    //check reference counts
    let location_la = get_account_data(&mut banks_client, location_la_pubkey)
        .await
        .expect("Unable to get Account")
        .get_location()
        .unwrap();
    assert_eq!(location_la.reference_count, 0);
    //check reference counts
    let location_ny = get_account_data(&mut banks_client, location_ny_pubkey)
        .await
        .expect("Unable to get Account")
        .get_location()
        .unwrap();
    assert_eq!(location_ny.reference_count, 1);
    //check reference counts
    let exchange = get_account_data(&mut banks_client, exchange_pubkey)
        .await
        .expect("Unable to get Account")
        .get_exchange()
        .unwrap();
    assert_eq!(exchange.reference_count, 1);

    println!("✅ Device initialized successfully",);
    /*****************************************************************************************************************************************************/
    println!("🟢 9. Activate Device...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs { resource_count: 2 }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
        ],
        &payer,
    )
    .await;

    let device = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    assert_eq!(device.account_type, AccountType::Device);
    assert_eq!(device.code, "la".to_string());
    assert_eq!(device.status, DeviceStatus::Activated);

    println!("✅ Device updated");
    /*****************************************************************************************************************************************************/
    println!("🟢 10. Update Device...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
            code: None,
            device_type: None,
            contributor_pk: None,
            public_ip: None,
            dz_prefixes: None,
            metrics_publisher_pk: None,
            mgmt_vrf: None,
            max_users: None,
            users_count: None,
            status: None,
            desired_status: None,
            resource_count: 0,
            reference_count: None,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_ny_pubkey, false),
            AccountMeta::new(location_ny_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let device_la = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    assert_eq!(device_la.account_type, AccountType::Device);
    assert_eq!(device_la.code, "la".to_string());
    assert_eq!(device_la.location_pk, location_ny_pubkey);
    assert_eq!(device_la.status, DeviceStatus::Activated);

    println!("✅ Device updated");
    /*****************************************************************************************************************************************************/
    println!("🟢 11. Update Device fail...");
    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateDevice(DeviceUpdateArgs {
            code: None,
            device_type: None,
            contributor_pk: None,
            public_ip: None,
            dz_prefixes: None,
            metrics_publisher_pk: None,
            mgmt_vrf: None,
            max_users: None,
            users_count: None,
            status: None,
            desired_status: None,
            resource_count: 0,
            reference_count: None,
        }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_la_pubkey, false),
            AccountMeta::new(location_ny_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(res.is_err());

    println!("✅ Device update failed correctly.");
    /*****************************************************************************************************************************************************/
    println!("🟢 12. Deleting Device...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteDevice(DeviceDeleteArgs {}),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let device_la = get_account_data(&mut banks_client, device_pubkey)
        .await
        .expect("Unable to get Account")
        .get_device()
        .unwrap();
    assert_eq!(device_la.account_type, AccountType::Device);
    assert_eq!(device_la.code, "la".to_string());
    assert_eq!(device_la.public_ip.to_string(), "8.8.8.8");
    assert_eq!(device_la.status, DeviceStatus::Deleting);

    /*****************************************************************************************************************************************************/
    println!("🟢 13. CloseAccount Device...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseAccountDevice(DeviceCloseAccountArgs { resource_count: 2 }),
        vec![
            AccountMeta::new(device_pubkey, false),
            AccountMeta::new(device.owner, false),
            AccountMeta::new(device.contributor_pk, false),
            AccountMeta::new(device.location_pk, false),
            AccountMeta::new(device.exchange_pk, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(config_pubkey, false),
            AccountMeta::new(tunnel_ids_pda, false),
            AccountMeta::new(dz_prefix_pda, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    let device_la = get_account_data(&mut banks_client, device_pubkey).await;
    assert_eq!(device_la, None);

    // check reference counts
    let contributor = get_account_data(&mut banks_client, contributor_pubkey)
        .await
        .expect("Unable to get Account")
        .get_contributor()
        .unwrap();
    assert_eq!(contributor.reference_count, 0);
    //check reference counts
    let location = get_account_data(&mut banks_client, location_la_pubkey)
        .await
        .expect("Unable to get Account")
        .get_location()
        .unwrap();
    assert_eq!(location.reference_count, 0);
    //check reference counts
    let exchange = get_account_data(&mut banks_client, exchange_pubkey)
        .await
        .expect("Unable to get Account")
        .get_exchange()
        .unwrap();
    assert_eq!(exchange.reference_count, 0);

    println!("✅ Device deleted successfully");
    println!("🟢🟢🟢  End test_device  🟢🟢🟢");
}
