mod common;

//

use doublezero_passport::{
    instruction::{
        account::DenyAccessAccounts, AccessMode, PassportInstructionData,
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

struct DenyAccessSetup {
    test_setup: common::ProgramTestWithOwner,
    sentinel_signer: Keypair,
    service_key: Pubkey,
    access_deposit: u64,
}

async fn setup_for_deny_access() -> DenyAccessSetup {
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

    DenyAccessSetup {
        test_setup,
        sentinel_signer: configured.sentinel_signer,
        service_key,
        access_deposit: 10_000_000,
    }
}

//
// Deny access — happy path.
//

#[tokio::test]
async fn test_deny_access() {
    let DenyAccessSetup {
        mut test_setup,
        sentinel_signer,
        service_key,
        access_deposit,
    } = setup_for_deny_access().await;

    let sentinel_before_balance = test_setup
        .banks_client
        .get_balance(sentinel_signer.pubkey())
        .await
        .unwrap();

    let (access_request_key, access_request) = test_setup.fetch_access_request(&service_key).await;

    let access_request_balance = test_setup
        .banks_client
        .get_balance(access_request_key)
        .await
        .unwrap();

    let request_rent = test_setup
        .banks_client
        .get_rent()
        .await
        .unwrap()
        .minimum_balance(zero_copy::data_end::<AccessRequest>());

    assert_eq!(access_request_balance - request_rent, access_deposit);
    assert_eq!(access_request.service_key, service_key);

    test_setup
        .deny_access(&sentinel_signer, &access_request_key)
        .await
        .unwrap();

    let sentinel_after_balance = test_setup
        .banks_client
        .get_balance(sentinel_signer.pubkey())
        .await
        .unwrap();

    assert_eq!(
        sentinel_before_balance + access_deposit + request_rent,
        sentinel_after_balance,
    );

    let access_request_info = test_setup
        .banks_client
        .get_account(access_request_key)
        .await
        .unwrap();
    assert!(access_request_info.is_none());
}

//
// Deny access — unauthorized sentinel.
//

#[tokio::test]
async fn test_cannot_deny_access_unauthorized_sentinel() {
    let DenyAccessSetup {
        mut test_setup,
        sentinel_signer,
        service_key,
        ..
    } = setup_for_deny_access().await;

    let sentinel_before_balance = test_setup
        .banks_client
        .get_balance(sentinel_signer.pubkey())
        .await
        .unwrap();

    let (access_request_key, _) = test_setup.fetch_access_request(&service_key).await;
    let unauthorized_signer = Keypair::new();

    let (tx_err, _) =
        simulate_deny_access_revert(&mut test_setup, &unauthorized_signer, &access_request_key)
            .await
            .unwrap();

    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );

    // Verify balances unchanged.
    let sentinel_after_balance = test_setup
        .banks_client
        .get_balance(sentinel_signer.pubkey())
        .await
        .unwrap();
    assert_eq!(sentinel_before_balance, sentinel_after_balance);

    // Verify access request still exists.
    let access_request_info = test_setup
        .banks_client
        .get_account(access_request_key)
        .await
        .unwrap();
    assert!(access_request_info.is_some());
}

//
// Helpers.
//

async fn simulate_deny_access_revert(
    test_setup: &mut common::ProgramTestWithOwner,
    sentinel_signer: &Keypair,
    access_request_key: &Pubkey,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let deny_access_ix = try_build_instruction(
        &ID,
        DenyAccessAccounts::new(&sentinel_signer.pubkey(), access_request_key),
        &PassportInstructionData::GrantAccess,
    )
    .unwrap();

    test_setup
        .unwrap_simulation_error(&[deny_access_ix], &[sentinel_signer])
        .await
}
