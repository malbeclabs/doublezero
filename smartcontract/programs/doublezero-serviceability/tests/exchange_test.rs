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
    let program_id = Pubkey::new_unique();
    let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    )
    .start()
    .await;

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
