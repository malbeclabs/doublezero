mod common;

//

use doublezero_revenue_distribution::{
    instruction::{ProgramConfiguration, ProgramFlagConfiguration},
    state::{self, CommunityBurnRateParameters, ProgramConfig},
    types::BurnRate,
    types::ValidatorFee,
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
    let should_pause = false;

    // Other settings.
    let accountant_key = Pubkey::new_unique();
    let contributor_manager_key = Pubkey::new_unique();
    let sol_2z_swap_program_id = Pubkey::new_unique();

    // Distribution settings.
    let calculation_grace_period_seconds = 6 * 60 * 60;

    // -- Solana validator fee parameters.
    let base_block_rewards = 500; // 5%
    let priority_block_rewards = 69; // 0.69%
    let inflation_rewards = 420; // 4.2%
    let jito_tips = 20; // 0.2%

    // -- Community burn rate.
    let initial_cbr = 100_000_000; // 10%.
    let cbr_limit = 500_000_000; // 50%.
    let dz_epochs_to_increasing_cbr = 10;
    let dz_epochs_to_cbr_limit = 20;

    // Relay settings.
    let prepaid_connection_termination_relay_lamports = 8 * 6_960;

    test_setup
        .configure_program(
            &admin_signer,
            [
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(should_pause)),
                ProgramConfiguration::Accountant(accountant_key),
                ProgramConfiguration::ContributorManager(contributor_manager_key),
                ProgramConfiguration::CalculationGracePeriodSeconds(
                    calculation_grace_period_seconds,
                ),
                ProgramConfiguration::Sol2zSwapProgram(sol_2z_swap_program_id),
                ProgramConfiguration::SolanaValidatorFeeParameters {
                    base_block_rewards,
                    priority_block_rewards,
                    inflation_rewards,
                    jito_tips,
                    _unused: [0; 32],
                },
                ProgramConfiguration::CommunityBurnRateParameters {
                    limit: cbr_limit,
                    dz_epochs_to_increasing: dz_epochs_to_increasing_cbr,
                    dz_epochs_to_limit: dz_epochs_to_cbr_limit,
                    initial_rate: Some(initial_cbr),
                },
                ProgramConfiguration::PrepaidConnectionTerminationRelayLamports(
                    prepaid_connection_termination_relay_lamports,
                ),
            ],
        )
        .await
        .unwrap();

    let (program_config_key, program_config, _) = test_setup.fetch_program_config().await;

    let mut expected_program_config = ProgramConfig::default();
    expected_program_config.bump_seed = ProgramConfig::find_address().1;
    expected_program_config.reserve_2z_bump_seed =
        state::find_2z_token_pda_address(&program_config_key).1;
    expected_program_config.admin_key = admin_signer.pubkey();
    expected_program_config.contributor_manager_key = contributor_manager_key;
    expected_program_config.set_is_paused(should_pause);
    expected_program_config.accountant_key = accountant_key;
    expected_program_config.sol_2z_swap_program_id = sol_2z_swap_program_id;

    let expected_distribution_params = &mut expected_program_config.distribution_parameters;
    expected_distribution_params.calculation_grace_period_seconds =
        calculation_grace_period_seconds;

    let expected_solana_validator_fee_params =
        &mut expected_distribution_params.solana_validator_fee_parameters;
    expected_solana_validator_fee_params.base_block_rewards =
        ValidatorFee::new(base_block_rewards).unwrap();
    expected_solana_validator_fee_params.priority_block_rewards =
        ValidatorFee::new(priority_block_rewards).unwrap();
    expected_solana_validator_fee_params.inflation_rewards =
        ValidatorFee::new(inflation_rewards).unwrap();
    expected_solana_validator_fee_params.jito_tips = ValidatorFee::new(jito_tips).unwrap();

    expected_distribution_params.community_burn_rate_parameters = CommunityBurnRateParameters::new(
        BurnRate::new(initial_cbr).unwrap(),
        BurnRate::new(cbr_limit).unwrap(),
        dz_epochs_to_increasing_cbr,
        dz_epochs_to_cbr_limit,
    )
    .unwrap();

    let expected_relay_params = &mut expected_program_config.relay_parameters;
    expected_relay_params.prepaid_connection_termination_lamports =
        prepaid_connection_termination_relay_lamports;
    assert_eq!(program_config, expected_program_config);
}
