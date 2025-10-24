use doublezero_serviceability::{
    entrypoint::*,
    instructions::*,
    pda::*,
    processors::{
        exchange::{create::*, delete::*, resume::*, suspend::*, update::*},
        globalconfig::set::SetGlobalConfigArgs,
    },
    state::{accounttype::AccountType, exchange::*},
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_exchange() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

    /***********************************************************************************************************************************/
    println!("🟢  Start test_exchange");

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
    println!("Initializing globalconfig account...");
    let (globalconfig_pubkey, _) = get_globalconfig_pda(&program_id);

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
            next_bgp_community: None,
        }),
        vec![
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;
    println!("✅ globalconfig account initialized");
    /***********************************************************************************************************************************/
    // Exchange _la

    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    println!("Testing Exchange initialization...");
    let globalstate_account = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_account.account_index, 0);

    let (exchange_pubkey, _) = get_exchange_pda(&program_id, globalstate_account.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
            code: "la".to_string(),
            name: "Los Angeles".to_string(),
            lat: 1.234,
            lng: 4.567,
            reserved: 0,
        }),
        vec![
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let exchange_la = get_account_data(&mut banks_client, exchange_pubkey)
        .await
        .expect("Unable to get Account")
        .get_exchange()
        .unwrap();
    assert_eq!(exchange_la.account_type, AccountType::Exchange);
    assert_eq!(exchange_la.code, "la".to_string());
    assert_eq!(exchange_la.status, ExchangeStatus::Activated);

    println!("✅ Exchange initialized successfully",);
    /*****************************************************************************************************************************************************/
    println!("Testing Exchange suspend...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SuspendExchange(ExchangeSuspendArgs {}),
        vec![
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let exchange_la = get_account_data(&mut banks_client, exchange_pubkey)
        .await
        .expect("Unable to get Account")
        .get_exchange()
        .unwrap();
    assert_eq!(exchange_la.account_type, AccountType::Exchange);
    assert_eq!(exchange_la.status, ExchangeStatus::Suspended);

    println!("✅ Exchange suspended");
    /*****************************************************************************************************************************************************/
    println!("Testing Exchange resumed...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::ResumeExchange(ExchangeResumeArgs {}),
        vec![
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let exchange = get_account_data(&mut banks_client, exchange_pubkey)
        .await
        .expect("Unable to get Account")
        .get_exchange()
        .unwrap();
    assert_eq!(exchange.account_type, AccountType::Exchange);
    assert_eq!(exchange.status, ExchangeStatus::Activated);

    println!("✅ Exchange resumed");
    /*****************************************************************************************************************************************************/
    println!("Testing Exchange update...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::UpdateExchange(ExchangeUpdateArgs {
            code: Some("la2".to_string()),
            name: Some("Los Angeles - Los Angeles".to_string()),
            lat: Some(3.433),
            lng: Some(23.223),
            bgp_community: Some(10500),
        }),
        vec![
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let exchange_la = get_account_data(&mut banks_client, exchange_pubkey)
        .await
        .expect("Unable to get Account")
        .get_exchange()
        .unwrap();
    assert_eq!(exchange_la.account_type, AccountType::Exchange);
    assert_eq!(exchange_la.code, "la2".to_string());
    assert_eq!(exchange_la.name, "Los Angeles - Los Angeles".to_string());
    assert_eq!(exchange_la.status, ExchangeStatus::Activated);

    println!("✅ Exchange updated");
    /*****************************************************************************************************************************************************/
    println!("Testing Exchange deletion...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::DeleteExchange(ExchangeDeleteArgs {}),
        vec![
            AccountMeta::new(exchange_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let exchange_la = get_account_data(&mut banks_client, exchange_pubkey).await;
    assert_eq!(exchange_la, None);

    println!("✅ Exchange deleted successfully");
    println!("🟢  End test_exchange");
}

#[tokio::test]
async fn test_exchange_bgp_community_autoassignment() {
    let program_id = Pubkey::new_unique();
    let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    )
    .start()
    .await;

    println!("🟢  Start test_exchange_bgp_community_autoassignment");

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
    let (globalconfig_pubkey, _) = get_globalconfig_pda(&program_id);

    println!("Initializing global state...");
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

    println!("Initializing globalconfig with next_bgp_community: None (defaults to 10000)...");
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
            next_bgp_community: None,
        }),
        vec![
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let globalconfig = get_account_data(&mut banks_client, globalconfig_pubkey)
        .await
        .expect("Unable to get GlobalConfig")
        .get_global_config()
        .unwrap();
    assert_eq!(globalconfig.next_bgp_community, 10000);
    println!("✅ GlobalConfig initialized with next_bgp_community: 10000");

    println!("Creating first exchange...");
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (exchange1_pubkey, _) = get_exchange_pda(&program_id, globalstate.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
            code: "nyc".to_string(),
            name: "New York".to_string(),
            lat: 40.7128,
            lng: -74.0060,
            reserved: 0,
        }),
        vec![
            AccountMeta::new(exchange1_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let exchange1 = get_account_data(&mut banks_client, exchange1_pubkey)
        .await
        .expect("Unable to get Exchange 1")
        .get_exchange()
        .unwrap();
    assert_eq!(exchange1.bgp_community, 10000);
    println!("✅ First exchange created with bgp_community: 10000");

    let globalconfig = get_account_data(&mut banks_client, globalconfig_pubkey)
        .await
        .expect("Unable to get GlobalConfig")
        .get_global_config()
        .unwrap();
    assert_eq!(globalconfig.next_bgp_community, 10001);
    println!("✅ GlobalConfig next_bgp_community incremented to: 10001");

    println!("Creating second exchange...");
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (exchange2_pubkey, _) = get_exchange_pda(&program_id, globalstate.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
            code: "lax".to_string(),
            name: "Los Angeles".to_string(),
            lat: 34.0522,
            lng: -118.2437,
            reserved: 0,
        }),
        vec![
            AccountMeta::new(exchange2_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let exchange2 = get_account_data(&mut banks_client, exchange2_pubkey)
        .await
        .expect("Unable to get Exchange 2")
        .get_exchange()
        .unwrap();
    assert_eq!(exchange2.bgp_community, 10001);
    println!("✅ Second exchange created with bgp_community: 10001");

    let globalconfig = get_account_data(&mut banks_client, globalconfig_pubkey)
        .await
        .expect("Unable to get GlobalConfig")
        .get_global_config()
        .unwrap();
    assert_eq!(globalconfig.next_bgp_community, 10002);
    println!("✅ GlobalConfig next_bgp_community incremented to: 10002");

    println!("Creating third exchange...");
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (exchange3_pubkey, _) = get_exchange_pda(&program_id, globalstate.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
            code: "sfo".to_string(),
            name: "San Francisco".to_string(),
            lat: 37.7749,
            lng: -122.4194,
            reserved: 0,
        }),
        vec![
            AccountMeta::new(exchange3_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let exchange3 = get_account_data(&mut banks_client, exchange3_pubkey)
        .await
        .expect("Unable to get Exchange 3")
        .get_exchange()
        .unwrap();
    assert_eq!(exchange3.bgp_community, 10002);
    println!("✅ Third exchange created with bgp_community: 10002");

    let globalconfig = get_account_data(&mut banks_client, globalconfig_pubkey)
        .await
        .expect("Unable to get GlobalConfig")
        .get_global_config()
        .unwrap();
    assert_eq!(globalconfig.next_bgp_community, 10003);
    println!("✅ GlobalConfig next_bgp_community incremented to: 10003");

    println!("Setting next_bgp_community to 10999 to test upper bound...");
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
            next_bgp_community: Some(10999),
        }),
        vec![
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let globalconfig = get_account_data(&mut banks_client, globalconfig_pubkey)
        .await
        .expect("Unable to get GlobalConfig")
        .get_global_config()
        .unwrap();
    assert_eq!(globalconfig.next_bgp_community, 10999);
    println!("✅ GlobalConfig updated to next_bgp_community: 10999");

    println!("Creating fourth exchange with bgp_community at upper bound (10999)...");
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (exchange4_pubkey, _) = get_exchange_pda(&program_id, globalstate.account_index + 1);

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
            code: "sea".to_string(),
            name: "Seattle".to_string(),
            lat: 47.6062,
            lng: -122.3321,
            reserved: 0,
        }),
        vec![
            AccountMeta::new(exchange4_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let exchange4 = get_account_data(&mut banks_client, exchange4_pubkey)
        .await
        .expect("Unable to get Exchange 4")
        .get_exchange()
        .unwrap();
    assert_eq!(exchange4.bgp_community, 10999);
    println!("✅ Fourth exchange created with bgp_community: 10999 (upper bound)");

    let globalconfig = get_account_data(&mut banks_client, globalconfig_pubkey)
        .await
        .expect("Unable to get GlobalConfig")
        .get_global_config()
        .unwrap();
    assert_eq!(globalconfig.next_bgp_community, 11000);
    println!("✅ GlobalConfig next_bgp_community incremented to: 11000 (exceeds valid range)");

    println!("Attempting to create fifth exchange with invalid bgp_community (11000)...");
    let globalstate = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    let (exchange5_pubkey, _) = get_exchange_pda(&program_id, globalstate.account_index + 1);

    let result = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
            code: "chi".to_string(),
            name: "Chicago".to_string(),
            lat: 41.8781,
            lng: -87.6298,
            reserved: 0,
        }),
        vec![
            AccountMeta::new(exchange5_pubkey, false),
            AccountMeta::new(globalconfig_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    assert!(result.is_err());
    let error = result.unwrap_err();
    let error_string = format!("{:?}", error);
    assert!(
        error_string.contains("Custom(55)"),
        "Expected error to contain 'Custom(55)' (InvalidBgpCommunity), but got: {}",
        error_string
    );
    println!(
        "✅ Fifth exchange creation failed as expected with error code 55 (InvalidBgpCommunity)"
    );

    println!("🟢  End test_exchange_bgp_community_autoassignment - All assertions passed!");
}
