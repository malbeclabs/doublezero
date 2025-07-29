mod common;

//

use doublezero_passport::state::ProgramConfig;
use solana_program_test::tokio;
use solana_sdk::{signature::Keypair, signer::Signer};

#[tokio::test]
async fn test_set_admin() {
    let mut test_setup = common::start_test().await;

    // Test input.

    let admin_signer = Keypair::new();

    test_setup
        .initialize_program()
        .await
        .unwrap()
        .set_admin(&admin_signer.pubkey())
        .await
        .unwrap();

    let (_, program_config) = test_setup.fetch_program_config().await;

    let mut expected_program_config = ProgramConfig::default();
    expected_program_config.admin_key = admin_signer.pubkey();
    assert_eq!(program_config, expected_program_config);
}
