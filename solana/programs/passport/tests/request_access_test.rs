mod common;

//

use common::process_instructions_for_test;
use doublezero_passport::{
    instruction::{
        account::RequestAccessAccounts, AccessMode, PassportInstructionData, ProgramConfiguration,
        ProgramFlagConfiguration, SolanaValidatorAttestation,
    },
    state::{AccessRequest, REQUEST_ACCESS_MAX_DATA_SIZE},
    ID,
};
use doublezero_program_tools::{instruction::try_build_instruction, zero_copy};
use solana_program_test::{tokio, BanksClientError};
use solana_pubkey::Pubkey;
use solana_sdk::{
    instruction::InstructionError,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};

//
// Setup.
//

struct RequestAccessSetup {
    test_setup: common::ProgramTestWithOwner,
    admin_signer: Keypair,
    request_deposit_lamports: u64,
    request_fee_lamports: u64,
    solana_validator_backup_ids_limit: u16,
}

async fn setup_for_request_access() -> RequestAccessSetup {
    let mut test_setup = common::start_test().await;

    let admin_signer = Keypair::new();
    let request_deposit_lamports = 10_000_000;
    let request_fee_lamports = 10_000;
    let solana_validator_backup_ids_limit = 2;

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
                ProgramConfiguration::SolanaValidatorBackupIdsLimit(
                    solana_validator_backup_ids_limit,
                ),
            ],
            &admin_signer,
        )
        .await
        .unwrap();

    RequestAccessSetup {
        test_setup,
        admin_signer,
        request_deposit_lamports,
        request_fee_lamports,
        solana_validator_backup_ids_limit,
    }
}

//
// Request access — exceeding backup IDs limit.
//

#[tokio::test]
async fn test_cannot_request_access_exceeding_backup_ids_limit() {
    let RequestAccessSetup {
        mut test_setup,
        solana_validator_backup_ids_limit,
        ..
    } = setup_for_request_access().await;

    let service_key = Pubkey::new_unique();
    let validator_id = Pubkey::new_unique();

    let attestation = SolanaValidatorAttestation {
        validator_id,
        service_key,
        ed25519_signature: [1; 64],
    };

    let (tx_err, program_logs) = simulate_request_access_revert(
        &mut test_setup,
        &service_key,
        AccessMode::SolanaValidatorWithBackupIds {
            attestation,
            backup_ids: vec![
                Pubkey::new_unique(),
                Pubkey::new_unique(),
                Pubkey::new_unique(),
            ],
        },
    )
    .await
    .unwrap();

    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidInstructionData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        &format!("Program log: Cannot exceed backup IDs limit {solana_validator_backup_ids_limit}",)
    );
}

//
// Request access — happy path with two access modes.
//

#[tokio::test]
async fn test_request_access() {
    let RequestAccessSetup {
        mut test_setup,
        request_deposit_lamports,
        request_fee_lamports,
        ..
    } = setup_for_request_access().await;

    let service_key_1 = Pubkey::new_unique();
    let service_key_2 = Pubkey::new_unique();
    let validator_id = Pubkey::new_unique();

    let attestation_1 = SolanaValidatorAttestation {
        validator_id,
        service_key: service_key_1,
        ed25519_signature: [1; 64],
    };
    let attestation_2 = SolanaValidatorAttestation {
        validator_id,
        service_key: service_key_2,
        ed25519_signature: [1; 64],
    };
    let backup_ids = vec![Pubkey::new_unique(), Pubkey::new_unique()];

    let access_mode_1 = AccessMode::SolanaValidator(attestation_1);
    let access_mode_2 = AccessMode::SolanaValidatorWithBackupIds {
        attestation: attestation_2,
        backup_ids: backup_ids.clone(),
    };

    test_setup
        .request_access(&service_key_1, access_mode_1.clone())
        .await
        .unwrap()
        .request_access(&service_key_2, access_mode_2.clone())
        .await
        .unwrap();

    // Verify first access request.
    let (access_request_key, access_request) =
        test_setup.fetch_access_request(&service_key_1).await;

    let mut encoded_access_mode = [0; REQUEST_ACCESS_MAX_DATA_SIZE];
    borsh::to_writer(encoded_access_mode.as_mut(), &access_mode_1).unwrap();

    let expected_access_request = AccessRequest {
        service_key: service_key_1,
        rent_beneficiary_key: test_setup.payer_signer.pubkey(),
        request_fee_lamports,
        encoded_access_mode,
    };
    assert_eq!(access_request, expected_access_request);

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

    // Verify second access request.
    let (access_request_key, access_request) =
        test_setup.fetch_access_request(&service_key_2).await;

    let mut encoded_access_mode = [0; REQUEST_ACCESS_MAX_DATA_SIZE];
    borsh::to_writer(encoded_access_mode.as_mut(), &access_mode_2).unwrap();

    let expected_access_request = AccessRequest {
        service_key: service_key_2,
        rent_beneficiary_key: test_setup.payer_signer.pubkey(),
        request_fee_lamports,
        encoded_access_mode,
    };
    assert_eq!(access_request, expected_access_request);

    let access_request_balance_after = test_setup
        .banks_client
        .get_balance(access_request_key)
        .await
        .unwrap();
    assert_eq!(
        access_request_balance_after,
        request_deposit_lamports + request_rent
    );

    // Fail on duplicate access request.
    let duplicate_ix = try_build_instruction(
        &ID,
        RequestAccessAccounts::new(&test_setup.payer_signer.pubkey(), &service_key_1),
        &PassportInstructionData::RequestAccess(AccessMode::SolanaValidator(attestation_1)),
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

    // Fail on mismatched service key.
    let payer_signer = Keypair::new();
    let (tx_err, _) = simulate_request_access_revert_with_payer(
        &mut test_setup,
        &payer_signer,
        &service_key_2,
        AccessMode::SolanaValidator(attestation_1),
    )
    .await
    .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidSeeds)
    );
}

//
// Request access — program paused.
//

#[tokio::test]
async fn test_cannot_request_access_when_paused() {
    let RequestAccessSetup {
        mut test_setup,
        admin_signer,
        ..
    } = setup_for_request_access().await;

    let service_key = Pubkey::new_unique();
    let validator_id = Pubkey::new_unique();

    let attestation = SolanaValidatorAttestation {
        validator_id,
        service_key,
        ed25519_signature: [1; 64],
    };

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

    let result = test_setup
        .request_access(&service_key, AccessMode::SolanaValidator(attestation))
        .await;
    assert!(result.is_err());
}

//
// Helpers.
//

async fn simulate_request_access_revert(
    test_setup: &mut common::ProgramTestWithOwner,
    service_key: &Pubkey,
    access_mode: AccessMode,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let payer_signer = Keypair::new();
    simulate_request_access_revert_with_payer(test_setup, &payer_signer, service_key, access_mode)
        .await
}

async fn simulate_request_access_revert_with_payer(
    test_setup: &mut common::ProgramTestWithOwner,
    payer_signer: &Keypair,
    service_key: &Pubkey,
    access_mode: AccessMode,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let ix = try_build_instruction(
        &ID,
        RequestAccessAccounts::new(&payer_signer.pubkey(), service_key),
        &PassportInstructionData::RequestAccess(access_mode),
    )
    .unwrap();

    test_setup
        .unwrap_simulation_error(&[ix], &[payer_signer])
        .await
}
