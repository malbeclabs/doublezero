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
// Initialize the access request
//

#[tokio::test]
async fn test_request_access() {
    let admin_signer = Keypair::new();

    let service_key = Pubkey::new_unique();
    let validator_id = Pubkey::new_unique();

    let access_deposit = 10_000_000;
    let access_fee = 10_000;

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
                    request_deposit_lamports: access_deposit,
                    request_fee_lamports: access_fee,
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
    assert_eq!(access_request_balance_after, access_deposit + request_rent);
    assert_eq!(access_request, expected_access_request);
}
