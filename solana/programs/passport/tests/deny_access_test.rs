mod common;

//

use doublezero_passport::{
    instruction::{ProgramConfiguration, ProgramFlagConfiguration},
    state::AccessRequest,
};
use doublezero_program_tools::zero_copy;
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::signature::{Keypair, Signer};

//
// Deny the access request
//

#[tokio::test]
async fn test_deny_access() {
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
                ProgramConfiguration::Sentinel(sentinel_signer.pubkey()),
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
