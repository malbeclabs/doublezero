mod common;

//

use doublezero_passport::{
    instruction::{
        account::GrantAccessAccounts, PassportInstructionData, ProgramConfiguration,
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
// Grant the access request
//

#[tokio::test]
async fn test_grant_access() {
    let admin_signer = Keypair::new();

    let service_key = Pubkey::new_unique();
    let validator_id = Pubkey::new_unique();
    let sentinel_signer = Keypair::new();

    let access_deposit = 10_000_000;
    let access_fee = 10_000;

    let mut test_setup = common::start_test().await;

    test_setup
        .transfer_lamports(&sentinel_signer.pubkey(), 128 * 6_960)
        .await
        .unwrap()
        .initialize_program()
        .await
        .unwrap()
        .set_admin(&admin_signer.pubkey())
        .await
        .unwrap()
        .configure_program(
            [
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
                ProgramConfiguration::DoubleZeroLedgerSentinel(sentinel_signer.pubkey()),
                ProgramConfiguration::AccessRequestDeposit {
                    request_deposit_lamports: access_deposit,
                    request_fee_lamports: access_fee,
                },
            ],
            &admin_signer,
        )
        .await
        .unwrap()
        .request_access(&service_key, &validator_id, [1u8; 64])
        .await
        .unwrap();

    // Test inputs

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

    //
    // Reject the grant access request with an unauthorized sentinel
    //

    test_setup
        .request_access(&service_key, &validator_id, [1u8; 64])
        .await
        .unwrap();

    // Test inputs

    let (access_request_key, _) = test_setup.fetch_access_request(&service_key).await;
    let unauthorized_signer = Keypair::new();

    // Cannot grant access with unauthorized sentinel
    let grant_access_ix = try_build_instruction(
        &ID,
        GrantAccessAccounts::new(
            &unauthorized_signer.pubkey(),
            &access_request_key,
            &test_setup.payer_signer.pubkey(),
        ),
        &PassportInstructionData::GrantAccess,
    )
    .unwrap();

    let (tx_err, _program_logs) = test_setup
        .unwrap_simulation_error(&[grant_access_ix], &[&unauthorized_signer])
        .await;

    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );

    let sentinel_after_balance = test_setup
        .banks_client
        .get_balance(sentinel_signer.pubkey())
        .await
        .unwrap();
    assert_eq!(sentinel_after_balance, 128 * 6_960 + access_fee); // Sentinel still has 10_000 from prior grant test

    let access_request_info = test_setup
        .banks_client
        .get_account(access_request_key)
        .await
        .unwrap();
    assert!(access_request_info.is_some());
}
