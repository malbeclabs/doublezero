mod common;

//

use doublezero_passport::state::ProgramConfig;
use doublezero_program_tools::zero_copy::checked_from_bytes_with_discriminator;
use solana_program_test::tokio;

//
// Setup.
//

struct InitializeProgramSetup {
    test_setup: common::ProgramTestWithOwner,
}

async fn setup_for_initialize_program() -> InitializeProgramSetup {
    let test_setup = common::start_test().await;
    InitializeProgramSetup { test_setup }
}

//
// Initialize program — happy path.
//

#[tokio::test]
async fn test_initialize_program() {
    let InitializeProgramSetup { mut test_setup } = setup_for_initialize_program().await;

    test_setup.initialize_program().await.unwrap();

    let (program_config_key, _) = ProgramConfig::find_address();

    let program_config_account_data = test_setup
        .banks_client
        .get_account(program_config_key)
        .await
        .unwrap()
        .unwrap()
        .data;

    let (program_config, remaining_data) =
        checked_from_bytes_with_discriminator::<ProgramConfig>(&program_config_account_data)
            .unwrap();
    assert!(remaining_data.is_empty());

    assert_eq!(program_config, &ProgramConfig::default());
}
