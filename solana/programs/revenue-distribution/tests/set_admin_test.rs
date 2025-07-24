mod common;

//

use doublezero_revenue_distribution::state::{self, ProgramConfig};
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

    let (program_config_key, program_config, _) = test_setup.fetch_program_config().await;

    let mut expected_program_config = ProgramConfig::default();
    expected_program_config.bump_seed = ProgramConfig::find_address().1;
    expected_program_config.reserve_2z_bump_seed =
        state::find_2z_token_pda_address(&program_config_key).1;
    expected_program_config.set_is_paused(true);
    expected_program_config.admin_key = admin_signer.pubkey();
    assert_eq!(program_config, expected_program_config);
}
