use doublezero_serviceability::{
    entrypoint::*,
    instructions::*,
    pda::*,
    processors::allowlist::device::{
        add::AddDeviceAllowlistArgs, remove::RemoveDeviceAllowlistArgs,
    },
    state::accounttype::AccountType,
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn device_allowlist_test() {
    let program_id = Pubkey::new_unique();
    let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    )
    .start()
    .await;

    /***********************************************************************************************************************************/
    println!("🟢  Start device_allowlist_test");

    let user1 = Pubkey::new_unique();
    let user2 = Pubkey::new_unique();

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

    /*****************************************************************************************************************************************************/
    println!("🟢 2. Add user1 to device allowlist...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddDeviceAllowlist(AddDeviceAllowlistArgs { pubkey: user1 }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let state = get_account_data(&mut banks_client, globalstate_pubkey)
        .await
        .expect("Unable to get Account")
        .get_global_state()
        .unwrap();

    assert_eq!(state.account_type, AccountType::GlobalState);
    assert_eq!(state.device_allowlist.len(), 2);
    assert!(state.device_allowlist.contains(&user1));

    println!("✅ Allowlist is correct");
    /*****************************************************************************************************************************************************/
    println!("🟢 3. Add user2 to device allowlist...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::AddDeviceAllowlist(AddDeviceAllowlistArgs { pubkey: user2 }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let state = get_account_data(&mut banks_client, globalstate_pubkey)
        .await
        .expect("Unable to get Account")
        .get_global_state()
        .unwrap();

    assert_eq!(state.account_type, AccountType::GlobalState);
    assert_eq!(state.device_allowlist.len(), 3);
    assert!(state.device_allowlist.contains(&user1));
    assert!(state.device_allowlist.contains(&user2));

    println!("✅ Allowlist is correct");
    /*****************************************************************************************************************************************************/
    println!("🟢 4. Remove user1 to device allowlist...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RemoveDeviceAllowlist(RemoveDeviceAllowlistArgs { pubkey: user1 }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let state = get_account_data(&mut banks_client, globalstate_pubkey)
        .await
        .expect("Unable to get Account")
        .get_global_state()
        .unwrap();

    assert_eq!(state.account_type, AccountType::GlobalState);
    assert_eq!(state.device_allowlist.len(), 2);
    assert!(!state.device_allowlist.contains(&user1));
    assert!(state.device_allowlist.contains(&user2));

    println!("✅ Allowlist is correct");
    /*****************************************************************************************************************************************************/
    println!("🟢 5. Remove user2 to device allowlist...");
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::RemoveDeviceAllowlist(RemoveDeviceAllowlistArgs { pubkey: user2 }),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let state = get_account_data(&mut banks_client, globalstate_pubkey)
        .await
        .expect("Unable to get Account")
        .get_global_state()
        .unwrap();

    assert_eq!(state.account_type, AccountType::GlobalState);
    assert_eq!(state.device_allowlist.len(), 1);
    assert!(!state.device_allowlist.contains(&user1));
    assert!(!state.device_allowlist.contains(&user2));

    println!("✅ Allowlist is correct");
    /*****************************************************************************************************************************************************/
    println!("🟢🟢🟢  End test_device_allowlist  🟢🟢🟢");
}
