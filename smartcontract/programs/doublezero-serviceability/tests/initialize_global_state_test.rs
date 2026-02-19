use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalstate_pda, get_program_config_pda},
    programversion::ProgramVersion,
    state::{accounttype::AccountType, globalstate::GlobalState, programconfig::ProgramConfig},
};
use solana_program::rent::Rent;
use solana_program_test::*;
use solana_sdk::{
    account::Account as SolanaAccount, instruction::AccountMeta, pubkey::Pubkey, signature::Signer,
    transaction::Transaction,
};
use std::str::FromStr;

#[tokio::test]
async fn test_initialize_global_state_resizes_programconfig_and_tops_up_rent() {
    let program_id = Pubkey::new_unique();
    let (program_config_pda, _) = get_program_config_pda(&program_id);
    let (globalstate_pda, globalstate_bump) = get_globalstate_pda(&program_id);

    let new_program_config = ProgramConfig {
        account_type: AccountType::ProgramConfig,
        bump_seed: 0,
        version: ProgramVersion::current(),
        min_compatible_version: ProgramVersion::from_str("1.0.0").unwrap(),
    };

    let required_space = borsh::object_length(&new_program_config).unwrap();
    let smaller_space = required_space / 2;

    let rent = Rent::default();
    let old_lamports = rent.minimum_balance(smaller_space);
    let new_required_lamports = rent.minimum_balance(required_space);

    let mut program_test = ProgramTest::new(
        "doublezero_serviceability",
        program_id,
        processor!(doublezero_serviceability::entrypoint::process_instruction),
    );

    program_test.add_account(
        program_config_pda,
        SolanaAccount {
            lamports: old_lamports,
            data: vec![0u8; smaller_space],
            owner: program_id,
            executable: false,
            rent_epoch: 0,
        },
    );

    let (banks_client, payer, recent_blockhash) = program_test.start().await;

    let ix = {
        let data = borsh::to_vec(&DoubleZeroInstruction::InitGlobalState()).unwrap();
        solana_sdk::instruction::Instruction {
            program_id,
            accounts: vec![
                AccountMeta::new(program_config_pda, false),
                AccountMeta::new(globalstate_pda, false),
                AccountMeta::new(payer.pubkey(), true),
                AccountMeta::new(solana_system_interface::program::ID, false),
            ],
            data,
        }
    };

    let mut tx = Transaction::new_with_payer(&[ix], Some(&payer.pubkey()));
    tx.sign(&[&payer], recent_blockhash);
    banks_client.process_transaction(tx).await.unwrap();

    let program_config_account = banks_client
        .get_account(program_config_pda)
        .await
        .unwrap()
        .expect("ProgramConfig account must exist");

    assert_eq!(
        program_config_account.data.len(),
        required_space,
        "ProgramConfig was not reallocated to the expected size"
    );

    assert!(
        program_config_account.lamports >= new_required_lamports,
        "ProgramConfig lamports were not topped up to rent-exempt minimum"
    );

    let stored_config =
        ProgramConfig::try_from(&program_config_account.data[..]).expect("valid ProgramConfig");
    assert_eq!(stored_config.account_type, AccountType::ProgramConfig);

    let globalstate_account = banks_client
        .get_account(globalstate_pda)
        .await
        .unwrap()
        .expect("GlobalState account must exist");

    let global_state =
        GlobalState::try_from(&globalstate_account.data[..]).expect("valid GlobalState");

    // basic shape
    assert_eq!(global_state.account_type, AccountType::GlobalState);
    assert_eq!(global_state.bump_seed, globalstate_bump);
    assert_eq!(global_state.account_index, 0);

    // authority / allowlists derived from payer in initialize_global_state()
    assert_eq!(global_state.foundation_allowlist, vec![payer.pubkey()]);
    assert_eq!(global_state._device_allowlist, vec![payer.pubkey()]);
    assert!(global_state._user_allowlist.is_empty());
    assert_eq!(global_state.activator_authority_pk, payer.pubkey());
    assert_eq!(global_state.sentinel_authority_pk, payer.pubkey());

    // airdrop defaults from initialize_global_state()
    assert_eq!(global_state.contributor_airdrop_lamports, 1_000_000_000);
    assert_eq!(global_state.user_airdrop_lamports, 40_000);
}
