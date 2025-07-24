use doublezero_serviceability::{
    entrypoint::*,
    instructions::*,
    pda::*,
    processors::allowlist::user::{add::AddUserAllowlistArgs, remove::RemoveUserAllowlistArgs},
    state::accounttype::AccountType,
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};
mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn user_allowlist_stress_test() {
    let program_id = Pubkey::new_unique();
    let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    )
    .start()
    .await;

    /***********************************************************************************************************************************/
    println!("🟢  Start user_allowlist_stress_test");

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
    println!("🟢 2. Add users to allowlist...");

    for i in 1..=950 {
        let user = Pubkey::new_unique();

        println!("Adding user #{i}");

        let recent_blockhash = banks_client
            .get_latest_blockhash()
            .await
            .expect("Failed to get latest blockhash");

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::AddUserAllowlist(AddUserAllowlistArgs { pubkey: user }),
            vec![AccountMeta::new(globalstate_pubkey, false)],
            &payer,
        )
        .await;
    }

    let state = get_account_data(&mut banks_client, globalstate_pubkey)
        .await
        .expect("Unable to get Account")
        .get_global_state()
        .unwrap();

    assert_eq!(state.account_type, AccountType::GlobalState);
    assert_eq!(state.user_allowlist.len(), 950);

    println!("✅ Allowlist is correct");
    /*****************************************************************************************************************************************************/
    println!("🟢 2. Remove users from allowlist...");

    for pk in state.user_allowlist.iter() {
        let recent_blockhash = banks_client
            .get_latest_blockhash()
            .await
            .expect("Failed to get latest blockhash");

        execute_transaction(
            &mut banks_client,
            recent_blockhash,
            program_id,
            DoubleZeroInstruction::RemoveUserAllowlist(RemoveUserAllowlistArgs { pubkey: *pk }),
            vec![AccountMeta::new(globalstate_pubkey, false)],
            &payer,
        )
        .await;
    }

    let state = get_account_data(&mut banks_client, globalstate_pubkey)
        .await
        .expect("Unable to get Account")
        .get_global_state()
        .unwrap();

    assert_eq!(state.account_type, AccountType::GlobalState);
    assert_eq!(state.user_allowlist.len(), 0);

    println!("✅ Allowlist is correct");

    /*****************************************************************************************************************************************************/
    println!("🟢🟢🟢  End user_allowlist_stress_test  🟢🟢🟢");
}
