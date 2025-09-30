mod common;

//

use doublezero_revenue_distribution::{
    instruction::{ProgramConfiguration, ProgramFlagConfiguration},
    state::{self, CommunityBurnRateParameters, Distribution, ProgramConfig},
    types::ValidatorFee,
    types::{BurnRate, DoubleZeroEpoch},
};
use solana_program_test::tokio;
use solana_sdk::signature::{Keypair, Signer};

//
// Initialize distribution.
//

#[tokio::test]
async fn test_initialize_distribution() {
    let mut test_setup = common::start_test().await;

    let admin_signer = Keypair::new();

    let debt_accountant_signer = Keypair::new();
    let solana_validator_base_block_rewards_pct_fee = 500; // 5%.
    let calculation_grace_period_minutes = 69;
    let initialization_grace_period_minutes = 420;

    // Relay settings.
    let distribute_rewards_relay_lamports = 10_000;

    // Community burn rate.
    let initial_cbr = 100_000_000; // 10%.
    let cbr_limit = 500_000_000; // 50%.
    let dz_epochs_to_increasing_cbr = 1;
    let dz_epochs_to_cbr_limit = 20;

    test_setup
        .initialize_program()
        .await
        .unwrap()
        .initialize_journal()
        .await
        .unwrap()
        .set_admin(&admin_signer.pubkey())
        .await
        .unwrap()
        .configure_program(
            &admin_signer,
            [
                ProgramConfiguration::DebtAccountant(debt_accountant_signer.pubkey()),
                ProgramConfiguration::SolanaValidatorFeeParameters {
                    base_block_rewards_pct: solana_validator_base_block_rewards_pct_fee,
                    priority_block_rewards_pct: 0,
                    inflation_rewards_pct: 0,
                    jito_tips_pct: 0,
                    fixed_sol_amount: 0,
                    _unused: Default::default(),
                },
                ProgramConfiguration::CommunityBurnRateParameters {
                    limit: cbr_limit,
                    dz_epochs_to_increasing: dz_epochs_to_increasing_cbr,
                    dz_epochs_to_limit: dz_epochs_to_cbr_limit,
                    initial_rate: Some(initial_cbr),
                },
                ProgramConfiguration::DistributeRewardsRelayLamports(
                    distribute_rewards_relay_lamports,
                ),
                ProgramConfiguration::CalculationGracePeriodMinutes(
                    calculation_grace_period_minutes,
                ),
                ProgramConfiguration::DistributionInitializationGracePeriodMinutes(
                    initialization_grace_period_minutes,
                ),
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
            ],
        )
        .await
        .unwrap()
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap();

    let mut cbr_params = CommunityBurnRateParameters::new(
        BurnRate::new(initial_cbr).unwrap(),
        BurnRate::new(cbr_limit).unwrap(),
        dz_epochs_to_increasing_cbr,
        dz_epochs_to_cbr_limit,
    )
    .unwrap();

    // Sync community burn rate.
    let expected_cbr = cbr_params.checked_compute().unwrap();
    assert_eq!(expected_cbr, BurnRate::new(100_000_000).unwrap());
    assert_eq!(
        cbr_params.next_burn_rate().unwrap(),
        BurnRate::new(120_000_000).unwrap()
    );

    let dz_epoch = DoubleZeroEpoch::new(0);
    let (distribution_key, distribution, _, _, distribution_custody) =
        test_setup.fetch_distribution(dz_epoch).await;

    let mut expected_distribution = Distribution::default();
    expected_distribution.bump_seed = Distribution::find_address(dz_epoch).1;
    expected_distribution.token_2z_pda_bump_seed =
        state::find_2z_token_pda_address(&distribution_key).1;
    expected_distribution.dz_epoch = dz_epoch;
    expected_distribution.community_burn_rate = expected_cbr;
    expected_distribution
        .solana_validator_fee_parameters
        .base_block_rewards_pct =
        ValidatorFee::new(solana_validator_base_block_rewards_pct_fee).unwrap();
    expected_distribution.distribute_rewards_relay_lamports = distribute_rewards_relay_lamports;
    expected_distribution.calculation_allowed_timestamp = test_setup
        .get_clock()
        .await
        .unix_timestamp
        .saturating_add(i64::from(calculation_grace_period_minutes) * 60)
        as u32;
    assert_eq!(distribution, expected_distribution);
    assert_eq!(distribution_custody.amount, 0);

    let (program_config_key, program_config, _) = test_setup.fetch_program_config().await;

    let mut expected_program_config = ProgramConfig::default();
    expected_program_config.bump_seed = ProgramConfig::find_address().1;
    expected_program_config.reserve_2z_bump_seed =
        state::find_2z_token_pda_address(&program_config_key).1;
    expected_program_config.admin_key = admin_signer.pubkey();
    expected_program_config.next_dz_epoch = DoubleZeroEpoch::new(1);
    expected_program_config.debt_accountant_key = debt_accountant_signer.pubkey();
    expected_program_config.last_initialized_distribution_timestamp =
        test_setup.get_clock().await.unix_timestamp as u32;

    let expected_distribution_params = &mut expected_program_config.distribution_parameters;
    expected_distribution_params.calculation_grace_period_minutes =
        calculation_grace_period_minutes;
    expected_distribution_params.initialization_grace_period_minutes =
        initialization_grace_period_minutes;
    expected_distribution_params
        .solana_validator_fee_parameters
        .base_block_rewards_pct =
        ValidatorFee::new(solana_validator_base_block_rewards_pct_fee).unwrap();
    expected_distribution_params.community_burn_rate_parameters = cbr_params;

    let expected_relay_params = &mut expected_program_config.relay_parameters;
    expected_relay_params.distribute_rewards_lamports = distribute_rewards_relay_lamports;
    assert_eq!(program_config, expected_program_config);

    // Create another distribution.

    test_setup
        .warp_timestamp_by(u32::from(initialization_grace_period_minutes) * 60)
        .await
        .unwrap()
        .initialize_distribution(&debt_accountant_signer)
        .await
        .unwrap();

    // Sync community burn rate.
    let expected_cbr = cbr_params.checked_compute().unwrap();
    assert_eq!(expected_cbr, BurnRate::new(120_000_000).unwrap());
    assert_eq!(
        cbr_params.next_burn_rate().unwrap(),
        BurnRate::new(140_000_000).unwrap()
    );

    let dz_epoch = DoubleZeroEpoch::new(1);
    let (distribution_key, distribution, _, _, distribution_custody) =
        test_setup.fetch_distribution(dz_epoch).await;

    let mut expected_distribution = Distribution::default();
    expected_distribution.bump_seed = Distribution::find_address(dz_epoch).1;
    expected_distribution.token_2z_pda_bump_seed =
        state::find_2z_token_pda_address(&distribution_key).1;
    expected_distribution.dz_epoch = dz_epoch;
    expected_distribution.community_burn_rate = expected_cbr;
    expected_distribution
        .solana_validator_fee_parameters
        .base_block_rewards_pct =
        ValidatorFee::new(solana_validator_base_block_rewards_pct_fee).unwrap();
    expected_distribution.distribute_rewards_relay_lamports = distribute_rewards_relay_lamports;
    expected_distribution.calculation_allowed_timestamp = test_setup
        .get_clock()
        .await
        .unix_timestamp
        .saturating_add(i64::from(calculation_grace_period_minutes) * 60)
        as u32;
    assert_eq!(distribution, expected_distribution);
    assert_eq!(distribution_custody.amount, 0);

    let (program_config_key, program_config, _) = test_setup.fetch_program_config().await;

    let mut expected_program_config = ProgramConfig::default();
    expected_program_config.bump_seed = ProgramConfig::find_address().1;
    expected_program_config.reserve_2z_bump_seed =
        state::find_2z_token_pda_address(&program_config_key).1;
    expected_program_config.admin_key = admin_signer.pubkey();
    expected_program_config.next_dz_epoch = DoubleZeroEpoch::new(2);
    expected_program_config.debt_accountant_key = debt_accountant_signer.pubkey();
    expected_program_config.last_initialized_distribution_timestamp =
        test_setup.get_clock().await.unix_timestamp as u32;

    let expected_distribution_params = &mut expected_program_config.distribution_parameters;
    expected_distribution_params.calculation_grace_period_minutes =
        calculation_grace_period_minutes;
    expected_distribution_params.initialization_grace_period_minutes =
        initialization_grace_period_minutes;
    expected_distribution_params
        .solana_validator_fee_parameters
        .base_block_rewards_pct =
        ValidatorFee::new(solana_validator_base_block_rewards_pct_fee).unwrap();
    expected_distribution_params.community_burn_rate_parameters = cbr_params;

    let expected_relay_params = &mut expected_program_config.relay_parameters;
    expected_relay_params.distribute_rewards_lamports = distribute_rewards_relay_lamports;
    assert_eq!(program_config, expected_program_config);
}
