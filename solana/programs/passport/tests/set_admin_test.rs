mod common;

//

use doublezero_passport::state::ProgramConfig;
use solana_program_test::tokio;
use solana_sdk::{signature::Keypair, signer::Signer};

//
// Setup.
//

struct SetAdminSetup {
    test_setup: common::ProgramTestWithOwner,
}

async fn setup_for_set_admin() -> SetAdminSetup {
    let mut test_setup = common::start_test().await;

    test_setup.initialize_program().await.unwrap();

    SetAdminSetup { test_setup }
}

//
// Set admin — happy path.
//

#[tokio::test]
async fn test_set_admin() {
    let SetAdminSetup { mut test_setup } = setup_for_set_admin().await;

    let admin_signer = Keypair::new();

    test_setup.set_admin(&admin_signer.pubkey()).await.unwrap();

    let (_, program_config) = test_setup.fetch_program_config().await;

    let mut expected_program_config = ProgramConfig::default();
    expected_program_config.admin_key = admin_signer.pubkey();
    assert_eq!(program_config, expected_program_config);
}
