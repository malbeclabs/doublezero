use doublezero_serviceability::{
    entrypoint::*,
    instructions::*,
    pda::*,
    processors::accesspass::{close::CloseAccessPassArgs, set::SetAccessPassArgs},
    state::accesspass::AccessPassType,
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signer::Signer};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_accesspass() {
    let program_id = Pubkey::new_unique();
    let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    )
    .start()
    .await;

    /***********************************************************************************************************************************/
    println!("🟢  Start test_accesspass");

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
    // AccessPass tests

    let client_ip = Ipv4Addr::new(100, 0, 0, 1);
    let user_payer = payer.pubkey();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);
    let solana_identity = Pubkey::new_unique();

    /***********************************************************************************************************************************/
    println!("🟢 1. Create AccessPass...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 10,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &payer,
    )
    .await;

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    assert_eq!(accesspass.accesspass_type, AccessPassType::Prepaid);
    assert_eq!(accesspass.client_ip, client_ip);
    assert_eq!(accesspass.last_access_epoch, 10);
    println!("✅ AccessPass created successfully");

    /***********************************************************************************************************************************/
    println!("🟢 2. Update AccessPass...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::SolanaValidator(solana_identity),
            client_ip,
            last_access_epoch: u64::MAX,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &payer,
    )
    .await;

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();
    assert_eq!(
        accesspass.accesspass_type,
        AccessPassType::SolanaValidator(solana_identity)
    );
    assert_eq!(accesspass.client_ip, client_ip);
    assert_eq!(accesspass.last_access_epoch, u64::MAX);
    println!("✅ AccessPass updated successfully");

    /***********************************************************************************************************************************/
    println!("🟢 3. Close AccessPass...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CloseAccessPass(CloseAccessPassArgs {}),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
        ],
        &payer,
    )
    .await;

    let accesspass_closed = get_account_data(&mut banks_client, accesspass_pubkey).await;
    assert!(accesspass_closed.is_none());

    println!("✅ AccessPass closed successfully");

    /***********************************************************************************************************************************/
    println!("🟢 4. Create AccessPass again...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 101,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &payer,
    )
    .await;

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();

    assert_eq!(accesspass.accesspass_type, AccessPassType::Prepaid);
    assert_eq!(accesspass.client_ip, client_ip);
    assert_eq!(accesspass.last_access_epoch, 101);
    println!("✅ AccessPass recreated successfully");

    /***********************************************************************************************************************************/
    println!("🟢 5. Update AccessPass last_epoch = 0...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 0,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &payer,
    )
    .await;

    let accesspass = get_account_data(&mut banks_client, accesspass_pubkey)
        .await
        .expect("Unable to get Account")
        .get_accesspass()
        .unwrap();

    assert_eq!(accesspass.accesspass_type, AccessPassType::Prepaid);
    assert_eq!(accesspass.client_ip, client_ip);
    assert_eq!(accesspass.last_access_epoch, 0);
    println!("✅ AccessPass update last_epoch successfully");

    /***********************************************************************************************************************************/

    println!("🟢  End test_accesspass");
}
