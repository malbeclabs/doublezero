mod common;

//

use doublezero_program_tools::zero_copy::checked_from_bytes_with_discriminator;
use doublezero_revenue_distribution::state::{self, ProgramConfig};
use solana_program_test::tokio;

#[tokio::test]
async fn test_initialize_program() {
    let mut test_setup = common::start_test().await;

    test_setup.initialize_program().await.unwrap();

    let (program_config_key, program_config_bump) = ProgramConfig::find_address();

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

    let mut expected_program_config = ProgramConfig::default();
    expected_program_config.bump_seed = program_config_bump;
    expected_program_config.reserve_2z_bump_seed =
        state::find_2z_token_pda_address(&program_config_key).1;
    expected_program_config.set_is_paused(true);
    assert_eq!(program_config, &expected_program_config);
}
