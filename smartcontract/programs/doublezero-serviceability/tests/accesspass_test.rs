use doublezero_serviceability::{
    instructions::*,
    pda::*,
    processors::accesspass::{
        check_status::CheckStatusAccessPassArgs, close::CloseAccessPassArgs, set::SetAccessPassArgs,
    },
    state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType},
        accounttype::AccountType,
    },
};
use solana_program::rent::Rent;
use solana_program_test::*;
use solana_sdk::{
    account::Account as SolanaAccount, instruction::AccountMeta, pubkey::Pubkey,
    signature::Keypair, signer::Signer, system_program,
};
use std::net::Ipv4Addr;

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_accesspass() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

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
    let user_payer = Pubkey::new_unique();
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
            allow_multiple_ip: false,
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
            allow_multiple_ip: false,
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
            allow_multiple_ip: false,
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
            allow_multiple_ip: false,
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
    println!("🟢 6. Check AccessPass...");

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CheckStatusAccessPass(CheckStatusAccessPassArgs {}),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
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
    println!("✅ AccessPass check Access Pass successfully");
    /***********************************************************************************************************************************/
    println!("🟢 6. Check AccessPass (no payer)...");

    let another_payer = Keypair::new();

    transfer(
        &mut banks_client,
        &payer,
        &another_payer.pubkey(),
        1_000_000_000,
    )
    .await;

    let res = try_execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::CheckStatusAccessPass(CheckStatusAccessPassArgs {}),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(payer.pubkey(), false),
            AccountMeta::new(system_program::id(), false),
        ],
        &another_payer,
    )
    .await;

    println!("res: {:?}", res);

    assert!(res.is_err());

    println!("✅ AccessPass check Access Pass fail successfully");
    /***********************************************************************************************************************************/

    println!("🟢  End test_accesspass");
}

#[tokio::test]
async fn test_close_accesspass_rejects_nonzero_connection_count() {
    // Set up a dedicated ProgramTest so we can pre-seed an AccessPass account
    let program_id = Pubkey::new_unique();

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    let client_ip = Ipv4Addr::new(101, 0, 0, 1);
    let user_payer = Pubkey::new_unique();
    let (accesspass_pubkey, bump_seed) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

    // Build an AccessPass with connection_count > 0
    let seeded_accesspass = AccessPass {
        account_type: AccountType::AccessPass,
        owner: program_id,
        bump_seed,
        accesspass_type: AccessPassType::Prepaid,
        client_ip,
        user_payer,
        last_access_epoch: 0,
        connection_count: 1,
        status: AccessPassStatus::Connected,
        mgroup_pub_allowlist: vec![],
        mgroup_sub_allowlist: vec![],
        flags: 0,
    };

    let accesspass_data = borsh::to_vec(&seeded_accesspass).unwrap();
    let rent = Rent::default();
    let lamports = rent.minimum_balance(accesspass_data.len());

    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(doublezero_serviceability::entrypoint::process_instruction),
    );

    // Pre-seed the AccessPass account owned by the program
    program_test.add_account(
        accesspass_pubkey,
        SolanaAccount {
            lamports,
            data: accesspass_data,
            owner: program_id,
            executable: false,
            rent_epoch: 0,
        },
    );

    let (mut banks_client, payer, recent_blockhash) = program_test.start().await;

    // Initialize global state so that payer is in the foundation_allowlist
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

    // Attempt to close the seeded AccessPass; this should fail because connection_count != 0
    let res = try_execute_transaction(
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

    assert!(
        res.is_err(),
        "CloseAccessPass should fail when connection_count > 0"
    );

    // The AccessPass account should still exist after the failed close attempt
    let account_after = banks_client.get_account(accesspass_pubkey).await.unwrap();
    assert!(account_after.is_some());
}

#[tokio::test]
async fn test_tx_lamports_to_pda_before_creation() {
    let (mut banks_client, program_id, payer, recent_blockhash) = init_test().await;

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
    let user_payer = Pubkey::new_unique();
    let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &client_ip, &user_payer);

    // Transfer lamports directly to the accesspass_pubkey
    test_helpers::transfer(&mut banks_client, &payer, &accesspass_pubkey, 128 * 6960).await;

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
            allow_multiple_ip: false,
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

    // Re-execute the same txn
    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip,
            last_access_epoch: 10,
            allow_multiple_ip: false,
        }),
        vec![
            AccountMeta::new(accesspass_pubkey, false),
            AccountMeta::new(globalstate_pubkey, false),
            AccountMeta::new(user_payer, false),
        ],
        &payer,
    )
    .await;
}
