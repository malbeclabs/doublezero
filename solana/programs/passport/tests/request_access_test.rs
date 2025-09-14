mod common;

//

use common::process_instructions_for_test;
use doublezero_passport::{
    instruction::{
        account::RequestAccessAccounts, AccessMode, PassportInstructionData, ProgramConfiguration,
        ProgramFlagConfiguration,
    },
    state::AccessRequest,
    ID,
};
use doublezero_program_tools::{instruction::try_build_instruction, zero_copy};
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::{
    instruction::InstructionError,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};

//
// Request access.
//

#[tokio::test]
async fn test_request_access() {
    let admin_signer = Keypair::new();

    let service_key = Pubkey::new_unique();
    let validator_id = Pubkey::new_unique();

    let request_deposit_lamports = 10_000_000;
    let request_fee_lamports = 10_000;

    let mut test_setup = common::start_test().await;

    test_setup
        .initialize_program()
        .await
        .unwrap()
        .set_admin(&admin_signer.pubkey())
        .await
        .unwrap()
        .configure_program(
            [
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
                ProgramConfiguration::AccessRequestDeposit {
                    request_deposit_lamports,
                    request_fee_lamports,
                },
            ],
            &admin_signer,
        )
        .await
        .unwrap();

    // Test inputs

    test_setup
        .request_access(&service_key, &validator_id, [1u8; 64])
        .await
        .unwrap();

    let (access_request_key, access_request) = test_setup.fetch_access_request(&service_key).await;

    let expected_access_request = AccessRequest {
        service_key,
        rent_beneficiary_key: test_setup.payer_signer.pubkey(),
        request_fee_lamports,
    };

    let request_rent = test_setup
        .banks_client
        .get_rent()
        .await
        .unwrap()
        .minimum_balance(zero_copy::data_end::<AccessRequest>());
    let access_request_balance_after = test_setup
        .banks_client
        .get_balance(access_request_key)
        .await
        .unwrap();
    assert_eq!(
        access_request_balance_after,
        request_deposit_lamports + request_rent
    );
    assert_eq!(access_request, expected_access_request);

    //
    // Fail on duplicate access request.
    //

    let duplicate_ix = try_build_instruction(
        &ID,
        RequestAccessAccounts::new(&test_setup.payer_signer.pubkey(), &service_key),
        &PassportInstructionData::RequestAccess(AccessMode::SolanaValidator {
            validator_id,
            service_key,
            ed25519_signature: [1u8; 64],
        }),
    )
    .unwrap();

    let recent_blockhash = test_setup.get_latest_blockhash().await.unwrap();
    let result = process_instructions_for_test(
        &mut test_setup.banks_client,
        &recent_blockhash,
        &[duplicate_ix],
        &[&test_setup.payer_signer],
    )
    .await;

    assert!(result.is_err());

    //
    // Fail on mismatched service key account and service key argument.
    //

    let wrong_service_key = Pubkey::new_unique();

    let payer_signer = Keypair::new();
    let invalid_service_key_ix = try_build_instruction(
        &ID,
        RequestAccessAccounts::new(&payer_signer.pubkey(), &wrong_service_key),
        &PassportInstructionData::RequestAccess(AccessMode::SolanaValidator {
            validator_id,
            service_key,
            ed25519_signature: [1u8; 64],
        }),
    )
    .unwrap();

    let (error, _) = test_setup
        .unwrap_simulation_error(&[invalid_service_key_ix], &[&payer_signer])
        .await;

    assert_eq!(
        error,
        TransactionError::InstructionError(0, InstructionError::InvalidSeeds)
    );

    //
    // Pause the program now.
    //

    test_setup
        .configure_program(
            [ProgramConfiguration::Flag(
                ProgramFlagConfiguration::IsPaused(true),
            )],
            &admin_signer,
        )
        .await
        .unwrap();

    let (_, program_config) = test_setup.fetch_program_config().await;
    assert!(program_config.is_paused());

    //
    // Request creation should error now.
    //

    let result = test_setup
        .request_access(&service_key, &validator_id, [1u8; 64])
        .await;

    assert!(result.is_err());
}
