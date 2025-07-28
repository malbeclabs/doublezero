use doublezero_serviceability::{
    entrypoint::*, instructions::DoubleZeroInstruction, pda::*,
    processors::globalstate::setinternetlatencycollector::SetInternetLatencyCollectorArgs,
};
use solana_program_test::*;
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signer::Signer};

mod test_helpers;
use test_helpers::*;

#[tokio::test]
async fn test_global_state() {
    let program_id = Pubkey::new_unique();
    let (mut banks_client, payer, recent_blockhash) = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(process_instruction),
    )
    .start()
    .await;

    /***********************************************************************************************************************************/
    println!("🟢 Start test_global_state");

    let (program_config_pubkey, _) = get_program_config_pda(&program_id);
    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    println!("🟢 Global Initialization...");
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
    // Set Latency Collector Oracle Agent

    let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);

    println!("Testing SetLatencyCollector...");
    let globalstate_acct = get_globalstate(&mut banks_client, globalstate_pubkey).await;
    assert_eq!(globalstate_acct.account_index, 0);

    assert_eq!(globalstate_acct.internet_latency_collector, payer.pubkey());

    let new_latency_collector = Pubkey::new_unique();

    let args = SetInternetLatencyCollectorArgs {
        pubkey: new_latency_collector,
    };

    execute_transaction(
        &mut banks_client,
        recent_blockhash,
        program_id,
        DoubleZeroInstruction::SetInternetLatencyCollector(args),
        vec![AccountMeta::new(globalstate_pubkey, false)],
        &payer,
    )
    .await;

    let globalstate_acct = get_globalstate(&mut banks_client, globalstate_pubkey).await;

    assert_eq!(
        globalstate_acct.internet_latency_collector,
        new_latency_collector
    );

    println!("✅ Latency Collector Oracle Agent updated successfully");
    println!("🟢 End test_global_state");
}
