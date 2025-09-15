mod common;

//

use doublezero_passport::{
    instruction::{ProgramConfiguration, ProgramFlagConfiguration},
    state::ProgramConfig,
};
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::signature::{Keypair, Signer};

//
// Configure program.
//

#[tokio::test]
async fn test_configure_program() {
    let mut test_setup = common::start_test().await;

    let admin_signer = Keypair::new();

    test_setup
        .initialize_program()
        .await
        .unwrap()
        .set_admin(&admin_signer.pubkey())
        .await
        .unwrap();

    // Test inputs.

    // Flags.
    let should_pause = true;

    // Other settings.
    let sentinel_key = Pubkey::new_unique();
    let required_deposit_lamports = 1_000_000;
    let fee_lamports = 1_000;

    test_setup
        .configure_program(
            [
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(should_pause)),
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsRequestAccessPaused(
                    should_pause,
                )),
                ProgramConfiguration::DoubleZeroLedgerSentinel(sentinel_key),
                ProgramConfiguration::AccessRequestDeposit {
                    request_deposit_lamports: required_deposit_lamports,
                    request_fee_lamports: fee_lamports,
                },
            ],
            &admin_signer,
        )
        .await
        .unwrap();

    let (_, program_config) = test_setup.fetch_program_config().await;

    let mut expected_program_config = ProgramConfig::default();
    expected_program_config.admin_key = admin_signer.pubkey();
    expected_program_config.set_is_paused(should_pause);
    expected_program_config.set_is_request_access_paused(should_pause);
    expected_program_config.sentinel_key = sentinel_key;
    expected_program_config.request_deposit_lamports = required_deposit_lamports;
    expected_program_config.request_fee_lamports = fee_lamports;

    assert_eq!(program_config, expected_program_config);
}
