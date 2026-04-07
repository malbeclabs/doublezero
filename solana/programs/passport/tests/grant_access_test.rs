mod common;

//

use doublezero_passport::{
    instruction::{
        account::GrantAccessAccounts, AccessMode, PassportInstructionData,
        SolanaValidatorAttestation,
    },
    state::AccessRequest,
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

struct GrantAccessSetup {
    test_setup: common::ProgramTestWithOwner,
    sentinel_signer: Keypair,
    service_key: Pubkey,
    access_deposit: u64,
    access_fee: u64,
}

async fn setup_for_grant_access() -> GrantAccessSetup {
    let mut test_setup = common::start_test().await;

    let configured = test_setup.setup_configured_program().await.unwrap();

    let service_key = Pubkey::new_unique();
    let validator_id = Pubkey::new_unique();

    let attestation = SolanaValidatorAttestation {
        validator_id,
        service_key,
        ed25519_signature: [1; 64],
    };

    test_setup
        .request_access(&service_key, AccessMode::SolanaValidator(attestation))
        .await
        .unwrap();

    GrantAccessSetup {
        test_setup,
        sentinel_signer: configured.sentinel_signer,
        service_key,
        access_deposit: 10_000_000,
        access_fee: 10_000,
    }
}

//
// Grant access — happy path.
//

#[tokio::test]
async fn test_grant_access() {
    let GrantAccessSetup {
        mut test_setup,
        sentinel_signer,
        service_key,
        access_deposit,
        access_fee,
    } = setup_for_grant_access().await;

    let sentinel_before_balance = test_setup
        .banks_client
        .get_balance(sentinel_signer.pubkey())
        .await
        .unwrap();
    let payer_before_balance = test_setup
        .banks_client
        .get_balance(test_setup.payer_signer.pubkey())
        .await
        .unwrap();

    let (access_request_key, access_request) = test_setup.fetch_access_request(&service_key).await;

    let request_rent = test_setup
        .banks_client
        .get_rent()
        .await
        .unwrap()
        .minimum_balance(zero_copy::data_end::<AccessRequest>());

    let access_request_balance = test_setup
        .banks_client
        .get_balance(access_request_key)
        .await
        .unwrap();
    assert_eq!(access_request_balance, access_deposit + request_rent);
    assert_eq!(access_request.service_key, service_key);

    test_setup
        .grant_access(
            &sentinel_signer,
            &access_request_key,
            &test_setup.payer_signer.pubkey(),
        )
        .await
        .unwrap();

    let sentinel_after_balance = test_setup
        .banks_client
        .get_balance(sentinel_signer.pubkey())
        .await
        .unwrap();
    let payer_after_balance = test_setup
        .banks_client
        .get_balance(test_setup.payer_signer.pubkey())
        .await
        .unwrap();

    assert_eq!(sentinel_before_balance + access_fee, sentinel_after_balance);

    let txn_signer_cost_adjustment = 10_000;
    let expected_payer_balance = payer_before_balance + access_deposit + request_rent
        - access_fee
        - txn_signer_cost_adjustment;
    assert_eq!(expected_payer_balance, payer_after_balance);

    let access_request_info = test_setup
        .banks_client
        .get_account(access_request_key)
        .await
        .unwrap();
    assert!(access_request_info.is_none());
}

//
// Grant access — unauthorized sentinel.
//

#[tokio::test]
async fn test_cannot_grant_access_unauthorized_sentinel() {
    let GrantAccessSetup {
        mut test_setup,
        service_key,
        ..
    } = setup_for_grant_access().await;

    let (access_request_key, _) = test_setup.fetch_access_request(&service_key).await;
    let unauthorized_signer = Keypair::new();
    let payer_key = test_setup.payer_signer.pubkey();

    let (tx_err, _) = simulate_grant_access_revert(
        &mut test_setup,
        &unauthorized_signer,
        &access_request_key,
        &payer_key,
    )
    .await
    .unwrap();

    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
}

//
// Grant access — wrong rent beneficiary.
//

#[tokio::test]
async fn test_cannot_grant_access_wrong_rent_beneficiary() {
    let GrantAccessSetup {
        mut test_setup,
        sentinel_signer,
        service_key,
        ..
    } = setup_for_grant_access().await;

    let (access_request_key, access_request) = test_setup.fetch_access_request(&service_key).await;

    let (tx_err, program_logs) = simulate_grant_access_revert(
        &mut test_setup,
        &sentinel_signer,
        &access_request_key,
        &Pubkey::new_unique(),
    )
    .await
    .unwrap();

    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(2).unwrap(),
        &format!(
            "Program log: Expected rent beneficiary key: {}",
            access_request.rent_beneficiary_key
        )
    );
}

//
// Helpers.
//

async fn simulate_grant_access_revert(
    test_setup: &mut common::ProgramTestWithOwner,
    sentinel_signer: &Keypair,
    access_request_key: &Pubkey,
    rent_beneficiary_key: &Pubkey,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let grant_access_ix = try_build_instruction(
        &ID,
        GrantAccessAccounts::new(
            &sentinel_signer.pubkey(),
            access_request_key,
            rent_beneficiary_key,
        ),
        &PassportInstructionData::GrantAccess,
    )
    .unwrap();

    test_setup
        .unwrap_simulation_error(&[grant_access_ix], &[sentinel_signer])
        .await
}
