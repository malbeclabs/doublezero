use doublezero_serviceability::{
    entrypoint::*,
    instructions::*,
    pda::*,
    processors::{
        contributor::create::ContributorCreateArgs,
        device::{activate::DeviceActivateArgs, create::*},
        exchange::create::*,
        globalconfig::set::SetGlobalConfigArgs,
        link::{activate::*, create::*},
        location::create::*,
        user::{activate::*, create::*},
    },
    state::{
        accounttype::AccountType, contributor::ContributorStatus, device::*, link::*, location::*,
        user::*,
    },
    types::*,
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signer};

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_doublezero_program() {
    let program_id = Pubkey::new_unique();
    let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    )
    .start()
    .await;

    /***********************************************************************************************************************************/
    println!("🟢  Start...");

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

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
    let (globalconfig_pubkey, _globalconfig_bump_seed) = get_globalconfig_pda(&program_id);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetGlobalConfig(SetGlobalConfigArgs {
            local_asn: 65000,
            remote_asn: 65001,
            device_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            user_tunnel_block: "10.0.0.0/24".parse().unwrap(),
            multicastgroup_block: "224.0.0.0/4".parse().unwrap(),
        }),
        vec![
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    /***********************************************************************************************************************************/
    // Location _la

    println!("Testing Location LA initialization...");
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 0);

    let location_la_code = "la".to_string();
    let (location_la_pubkey, _) =
        get_location_pda(&program_id, globalstate_account.account_index + 1);
    let location_la: LocationCreateArgs = LocationCreateArgs {
        code: location_la_code.clone(),
        name: "Los Angeles".to_string(),
        country: "us".to_string(),
        lat: 1.234,
        lng: 4.567,
        loc_id: 0,
    };

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(location_la),
        vec![
            AccountMeta::new(location_la_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let location_la = get_account_data(&mut banks_client, location_la_pubkey)
        .await
        .expect("Unable to get Location")
        .get_location()
        .unwrap();
    assert_eq!(location_la.account_type, AccountType::Location);
    assert_eq!(location_la.code, location_la_code);
    assert_eq!(location_la.status, LocationStatus::Activated);

    println!(
        "✅ Location LA initialized successfully with index: {}",
        location_la.index
    );

    println!("Testing Location NY initialization...");
    // Location _la

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 1);

    let location_ny_code = "ny".to_string();
    let (location_ny_pubkey, _) =
        get_location_pda(&program_id, globalstate_account.account_index + 1);
    let location_ny: LocationCreateArgs = LocationCreateArgs {
        code: location_ny_code.clone(),
        name: "New York".to_string(),
        country: "us".to_string(),
        lat: 1.234,
        lng: 4.567,
        loc_id: 0,
    };

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLocation(location_ny),
        vec![
            AccountMeta::new(location_ny_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let location_ny = get_account_data(&mut banks_client, location_ny_pubkey)
        .await
        .expect("Unable to get Location")
        .get_location()
        .unwrap();
    assert_eq!(location_ny.account_type, AccountType::Location);
    assert_eq!(location_ny.code, location_ny_code);
    println!(
        "✅ Location initialized successfully with index: {}",
        location_ny.index
    );

    /***********************************************************************************************************************************/

    // Exchange _la
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 2);

    let exchange_la_code = "la".to_string();
    let (exchange_la_pubkey, _) =
        get_exchange_pda(&program_id, globalstate_account.account_index + 1);
    let exchange_la: ExchangeCreateArgs = ExchangeCreateArgs {
        code: exchange_la_code.clone(),
        name: "Los Angeles".to_string(),
        lat: 1.234,
        lng: 4.567,
        loc_id: 0,
    };

    println!("Testing Exchange LA initialization...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(exchange_la),
        vec![
            AccountMeta::new(exchange_la_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let exchange_la = get_account_data(&mut banks_client, exchange_la_pubkey)
        .await
        .expect("Unable to get Exchange")
        .get_exchange()
        .unwrap();
    assert_eq!(exchange_la.account_type, AccountType::Exchange);
    assert_eq!(exchange_la.code, exchange_la_code);
    println!(
        "✅ Exchange initialized successfully with index: {}",
        exchange_la.index
    );

    println!("Testing Exchange NY initialization...");
    // Exchange _ny
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 3);

    let exchange_ny_code = "ny".to_string();
    let (exchange_ny_pubkey, _) =
        get_exchange_pda(&program_id, globalstate_account.account_index + 1);
    let exchange_ny: ExchangeCreateArgs = ExchangeCreateArgs {
        code: exchange_ny_code.clone(),
        name: "New York".to_string(),
        lat: 1.234,
        lng: 4.567,
        loc_id: 0,
    };

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(exchange_ny),
        vec![
            AccountMeta::new(exchange_ny_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let exchange_ny = get_account_data(&mut banks_client, exchange_ny_pubkey)
        .await
        .expect("Unable to get Exchange")
        .get_exchange()
        .unwrap();

    assert_eq!(exchange_ny.account_type, AccountType::Exchange);
    assert_eq!(exchange_ny.code, exchange_ny_code);
    println!(
        "✅ Exchange initialized successfully with index: {}",
        exchange_ny.index
    );

    /***********************************************************************************************************************************/
    println!("🟢 5. Create Contributor...");
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 4);

    let (contributor_pubkey, _) =
        get_contributor_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateContributor(ContributorCreateArgs {
            code: "cont".to_string(),
            owner: payer.pubkey(),
        }),
        vec![
            AccountMeta::new(contributor_pubkey, false),
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
    assert_eq!(contributor.status, ContributorStatus::Activated);

    println!("✅ Contributor initialized successfully",);
    /***********************************************************************************************************************************/

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 5);

    // Device _la
    let device_la_code = "la1".to_string();
    let (device_la_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
    let device_la: DeviceCreateArgs = DeviceCreateArgs {
        code: device_la_code.clone(),
        device_type: DeviceType::Switch,
        public_ip: [1, 0, 0, 1].into(),
        dz_prefixes: NetworkV4List::default(),
        metrics_publisher_pk: Pubkey::default(), // Assuming no metrics publisher for this test
        mgmt_vrf: "mgmt".to_string(),
        interfaces: vec![Interface::V1(CurrentInterfaceVersion {
            name: "eth0".to_string(),
            ..CurrentInterfaceVersion::default()
        })],
    };

    println!("Testing Device LA initialization...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(device_la),
        vec![
            AccountMeta::new(device_la_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_la_pubkey, false),
            AccountMeta::new(exchange_la_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let device_la = get_account_data(&mut banks_client, device_la_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();
    assert_eq!(device_la.account_type, AccountType::Device);
    assert_eq!(device_la.code, device_la_code);
    println!(
        "✅ Device LA initialized successfully with index: {}",
        device_la.index
    );

    println!("Testing Device NY initialization...");
    // Device _ny
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 6);

    let device_ny_code = "ny1".to_string();
    let (device_ny_pubkey, _) = get_device_pda(&program_id, globalstate_account.account_index + 1);
    let device_ny: DeviceCreateArgs = DeviceCreateArgs {
        code: device_ny_code.clone(),
        device_type: DeviceType::Switch,
        public_ip: [1, 0, 0, 2].into(),
        dz_prefixes: vec!["10.1.0.1/24".parse().unwrap()].into(),
        metrics_publisher_pk: Pubkey::default(), // Assuming no metrics publisher for this test
        mgmt_vrf: "mgmt".to_string(),
        interfaces: vec![Interface::V1(CurrentInterfaceVersion {
            name: "eth1".to_string(),
            ..CurrentInterfaceVersion::default()
        })],
    };

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateDevice(device_ny),
        vec![
            AccountMeta::new(device_ny_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(location_ny_pubkey, false),
            AccountMeta::new(exchange_ny_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let device_ny = get_account_data(&mut banks_client, device_ny_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();
    assert_eq!(device_ny.account_type, AccountType::Device);
    assert_eq!(device_ny.code, device_ny_code);
    println!(
        "✅ Device NY initialized successfully with index: {}",
        device_ny.index
    );

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs),
        vec![
            AccountMeta::new(device_ny_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let device_ny = get_account_data(&mut banks_client, device_ny_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();
    assert_eq!(device_ny.status, DeviceStatus::Activated);
    println!(
        "✅ Device NY activation successfully with index: {}",
        device_ny.index
    );

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateDevice(DeviceActivateArgs),
        vec![
            AccountMeta::new(device_la_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let device_la = get_account_data(&mut banks_client, device_la_pubkey)
        .await
        .expect("Unable to get Device")
        .get_device()
        .unwrap();
    assert_eq!(device_la.status, DeviceStatus::Activated);
    println!(
        "✅ Device LA activation successfully with index: {}",
        device_ny.index
    );

    /***********************************************************************************************************************************/

    // Device _la
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 7);

    let tunnel_la_ny_code = "la-ny1".to_string();
    let (tunnel_la_ny_pubkey, _) = get_link_pda(&program_id, globalstate_account.account_index + 1);
    let tunnel_la_ny: LinkCreateArgs = LinkCreateArgs {
        code: tunnel_la_ny_code.clone(),
        link_type: LinkLinkType::WAN,
        bandwidth: 100,
        mtu: 1900,
        delay_ns: 12_000_000,
        jitter_ns: 1_000_000,
        side_a_iface_name: "eth0".to_string(),
        side_z_iface_name: Some("eth1".to_string()),
    };

    println!("Testing Link LA-NY initialization...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateLink(tunnel_la_ny),
        vec![
            AccountMeta::new(tunnel_la_ny_pubkey, false),
            AccountMeta::new(contributor_pubkey, false),
            AccountMeta::new(device_la_pubkey, false),
            AccountMeta::new(device_ny_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Check account data
    let tunnel = get_account_data(&mut banks_client, tunnel_la_ny_pubkey)
        .await
        .expect("Unable to get Link")
        .get_tunnel()
        .unwrap();

    assert_eq!(tunnel.account_type, AccountType::Link);
    assert_eq!(tunnel.code, tunnel_la_ny_code);
    assert_eq!(tunnel.status, LinkStatus::Pending);
    println!(
        "✅ Link LA-NY initialized successfully with index: {}",
        tunnel.index
    );

    println!("Testing Link activation...");
    let tunnel_net: NetworkV4 = "10.31.0.0/31".parse().unwrap();
    let tunnel_activate: LinkActivateArgs = LinkActivateArgs {
        tunnel_id: 1,
        tunnel_net,
    };

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateLink(tunnel_activate),
        vec![
            AccountMeta::new(tunnel_la_ny_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
        ],
        &payer,
    )
    .await;

    // Check account data
    let tunnel = get_account_data(&mut banks_client, tunnel_la_ny_pubkey)
        .await
        .expect("Unable to get Link")
        .get_tunnel()
        .unwrap();

    assert_eq!(tunnel.account_type, AccountType::Link);
    assert_eq!(tunnel.code, tunnel_la_ny_code);
    assert_eq!(tunnel.status, LinkStatus::Activated);
    println!("✅ Link LA-NY activated successfully with value: {tunnel_net:?}",);

    println!("Start Users...");
    /***********************************************************************************************************************************/

    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 8);

    let user_ip = "100.0.0.1".parse().unwrap();
    let (user1_pubkey, _) = get_user_pda(&program_id, globalstate_account.account_index + 1);
    let user1: UserCreateArgs = UserCreateArgs {
        user_type: UserType::IBRL,
        cyoa_type: UserCYOA::GREOverDIA,
        client_ip: user_ip,
    };

    println!("Testing User1 initialization...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateUser(user1),
        vec![
            AccountMeta::new(user1_pubkey, false),
            AccountMeta::new(device_la_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Check account data
    let user1 = get_account_data(&mut banks_client, user1_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();
    assert_eq!(user1.account_type, AccountType::User);
    assert_eq!(user1.status, UserStatus::Pending);
    println!(
        "✅ User initialized successfully with index: {}",
        user1.index
    );

    let tunnel_net: NetworkV4 = "10.0.0.0/24".parse().unwrap();
    let dz_ip: std::net::Ipv4Addr = "10.2.0.1".parse().unwrap();
    let validator_pubkey = Pubkey::new_unique();

    let update1: UserActivateArgs = UserActivateArgs {
        tunnel_id: 1,
        tunnel_net,
        dz_ip,
        validator_pubkey: Some(validator_pubkey),
    };

    println!("Testing User1 activation...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ActivateUser(update1),
        vec![
            AccountMeta::new(user1_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    // Check account data
    let user1 = get_account_data(&mut banks_client, user1_pubkey)
        .await
        .expect("Unable to get User")
        .get_user()
        .unwrap();

    assert_eq!(user1.account_type, AccountType::User);
    assert_eq!(user1.tunnel_id, 1);
    assert_eq!(user1.tunnel_net, tunnel_net);
    assert_eq!(user1.dz_ip, dz_ip);
    assert_eq!(user1.validator_pubkey, validator_pubkey);
    assert_eq!(user1.status, UserStatus::Activated);
    println!(
        "✅ User initialized successfully with index: {}",
        user1.index
    );
}
