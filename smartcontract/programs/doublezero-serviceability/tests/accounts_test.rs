use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalstate_pda, get_program_config_pda},
    programversion::ProgramVersion,
    state::{accounttype::AccountType, programconfig::ProgramConfig},
};
use solana_program::rent::Rent;
use solana_program_test::*;
use solana_sdk::{
    account::Account as SolanaAccount, instruction::AccountMeta, pubkey::Pubkey, signature::Signer,
    transaction::Transaction,
};
use std::str::FromStr;

#[tokio::test]
async fn test_write_account_realloc_funds_from_payer() {
    let program_id = Pubkey::new_unique();
    let (program_config_pda, _) = get_program_config_pda(&program_id);
    let (globalstate_pda, _) = get_globalstate_pda(&program_id);

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

    let payer_before = banks_client
        .get_account(payer.pubkey())
        .await
        .unwrap()
        .expect("payer must exist")
        .lamports;

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

    let payer_after = banks_client
        .get_account(payer.pubkey())
        .await
        .unwrap()
        .expect("payer must exist")
        .lamports;

    let payer_delta = payer_before - payer_after;
    let funded_delta = program_config_account.lamports - old_lamports;

    // payer must have paid at least as much as went into ProgramConfig
    assert!(
        payer_delta >= funded_delta,
        "payer should lose at least as many lamports as were added to ProgramConfig"
    );

    // ProgramConfig should now be rent-exempt at the new size
    assert!(
        program_config_account.lamports >= new_required_lamports,
        "ProgramConfig lamports were not topped up to rent-exempt minimum"
    );

    // And size should match required_space
    assert_eq!(
        program_config_account.data.len(),
        required_space,
        "ProgramConfig was not reallocated to the expected size"
    );
}
