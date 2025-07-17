mod common;

use doublezero_program_tools::zero_copy::checked_from_bytes_with_discriminator;
use doublezero_revenue_distribution::{
    instruction::ConfigureFlag,
    types::{BurnRate, DoubleZeroEpoch},
    {
        instruction::{AdminKey, ConfigureDistributionData, ConfigureProgramSetting},
        state::{self, CommunityBurnRateParameters, Distribution, Journal, ProgramConfig},
        types::ValidatorFee,
        DOUBLEZERO_MINT,
    },
};
use solana_hash::Hash;
use solana_program_pack::Pack;
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::signature::{Keypair, Signer};
use spl_token::state::{Account as TokenAccount, AccountState as SplTokenAccountState};

//
// Initialize program.
//

#[tokio::test]
async fn test_initialize_program() {
    let mut test_setup = common::start_test().await;

    test_setup.initialize_program().await.unwrap();

    let program_config_account_data = test_setup
        .banks_client
        .get_account(ProgramConfig::find_address().0)
        .await
        .unwrap()
        .unwrap()
        .data;

    let (program_config, remaining_data) =
        checked_from_bytes_with_discriminator::<ProgramConfig>(&program_config_account_data)
            .unwrap();
    assert!(remaining_data.is_empty());

    let mut expected_program_config = ProgramConfig::default();
    expected_program_config.set_is_paused(true);
    assert_eq!(program_config, &expected_program_config);
}

//
// Set admin.
//

#[tokio::test]
async fn test_set_admin() {
    let mut test_setup = common::start_test().await;

    // Test input.

    let admin_signer = Keypair::new();

    test_setup
        .initialize_program()
        .await
        .unwrap()
        .set_admin(AdminKey::new(admin_signer.pubkey()))
        .await
        .unwrap();

    let (_, program_config) = test_setup.fetch_program_config().await;

    let mut expected_program_config = ProgramConfig::default();
    expected_program_config.set_is_paused(true);
    expected_program_config.admin_key = admin_signer.pubkey();
    assert_eq!(program_config, expected_program_config);
}

//
// Configure program.
//

#[tokio::test]
async fn test_configure_program() {
    let mut test_setup = common::start_test().await;

    let admin_signer = Keypair::new();

    // Test inputs.

    // Flags.
    let should_pause = false;

    // Other settings.
    let accountant_key = Pubkey::new_unique();
    let sol_2z_swap_program_id = Pubkey::new_unique();
    let solana_validator_fee = 500; // 5%
    let calculation_grace_period_seconds = 6 * 60 * 60;

    // Community burn rate.
    let initial_cbr = 100_000_000; // 10%
    let cbr_limit = 500_000_000; // 50%
    let dz_epochs_to_increasing_cbr = 10;
    let dz_epochs_to_cbr_limit = 20;

    test_setup
        .initialize_program()
        .await
        .unwrap()
        .set_admin(AdminKey::new(admin_signer.pubkey()))
        .await
        .unwrap()
        .configure_program(
            [
                ConfigureProgramSetting::Flag(ConfigureFlag::IsPaused(should_pause)),
                ConfigureProgramSetting::Accountant(accountant_key),
                ConfigureProgramSetting::CalculationGracePeriodSeconds(
                    calculation_grace_period_seconds,
                ),
                ConfigureProgramSetting::Sol2zSwapProgram(sol_2z_swap_program_id),
                ConfigureProgramSetting::SolanaValidatorFee(solana_validator_fee),
                ConfigureProgramSetting::CommunityBurnRateParameters {
                    limit: cbr_limit,
                    dz_epochs_to_increasing: dz_epochs_to_increasing_cbr,
                    dz_epochs_to_limit: dz_epochs_to_cbr_limit,
                    initial_rate: Some(initial_cbr),
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
    expected_program_config.accountant_key = accountant_key;
    expected_program_config.calculation_grace_period_seconds = calculation_grace_period_seconds;
    expected_program_config.sol_2z_swap_program_id = sol_2z_swap_program_id;
    expected_program_config.current_solana_validator_fee =
        ValidatorFee::new(solana_validator_fee).unwrap();
    expected_program_config.community_burn_rate_parameters = CommunityBurnRateParameters::new(
        BurnRate::new(initial_cbr).unwrap(),
        BurnRate::new(cbr_limit).unwrap(),
        dz_epochs_to_increasing_cbr,
        dz_epochs_to_cbr_limit,
    )
    .unwrap();
    assert_eq!(program_config, expected_program_config);
}

//
// Initialize journal.
//

#[tokio::test]
async fn test_initialize_journal() {
    let mut test_setup = common::start_test().await;

    test_setup.initialize_journal().await.unwrap();

    let journal_key = Journal::find_address().0;
    let journal_account_data = test_setup
        .banks_client
        .get_account(journal_key)
        .await
        .unwrap()
        .unwrap()
        .data;

    let (journal, remaining_data) =
        checked_from_bytes_with_discriminator::<Journal>(&journal_account_data).unwrap();
    assert_eq!(journal, &Journal::default());

    let epoch_payments = Journal::checked_epoch_payments(remaining_data).unwrap();
    assert!(epoch_payments.is_empty());

    let custodied_2z_token_account_data = test_setup
        .banks_client
        .get_account(state::find_custodied_2z_address(&journal_key).0)
        .await
        .unwrap()
        .unwrap()
        .data;
    let custodied_2z_token_account =
        TokenAccount::unpack(&custodied_2z_token_account_data).unwrap();
    let expected_custodied_2z_token_account = TokenAccount {
        mint: DOUBLEZERO_MINT,
        owner: journal_key,
        state: SplTokenAccountState::Initialized,
        ..Default::default()
    };
    assert_eq!(
        custodied_2z_token_account,
        expected_custodied_2z_token_account
    );
}

//
// Initialize distribution.
//

#[tokio::test]
async fn test_initialize_distribution() {
    let mut test_setup = common::start_test().await;

    let admin_signer = Keypair::new();

    let accountant_signer = Keypair::new();
    let solana_validator_fee = 500; // 5%

    // Community burn rate.
    let initial_cbr = 100_000_000; // 10%
    let cbr_limit = 500_000_000; // 50%
    let dz_epochs_to_increasing_cbr = 1;
    let dz_epochs_to_cbr_limit = 20;

    test_setup
        .initialize_program()
        .await
        .unwrap()
        .set_admin(AdminKey::new(admin_signer.pubkey()))
        .await
        .unwrap()
        .configure_program(
            [
                ConfigureProgramSetting::Accountant(accountant_signer.pubkey()),
                ConfigureProgramSetting::SolanaValidatorFee(solana_validator_fee),
                ConfigureProgramSetting::CommunityBurnRateParameters {
                    limit: cbr_limit,
                    dz_epochs_to_increasing: dz_epochs_to_increasing_cbr,
                    dz_epochs_to_limit: dz_epochs_to_cbr_limit,
                    initial_rate: Some(initial_cbr),
                },
                ConfigureProgramSetting::Flag(ConfigureFlag::IsPaused(false)),
            ],
            &admin_signer,
        )
        .await
        .unwrap()
        .initialize_distribution(&accountant_signer)
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
    let (_, distribution, distribution_custody) = test_setup.fetch_distribution(dz_epoch).await;

    let mut expected_distribution = Distribution::default();
    expected_distribution.dz_epoch = dz_epoch;
    expected_distribution.community_burn_rate = expected_cbr;
    assert_eq!(distribution, expected_distribution);
    assert_eq!(distribution_custody.amount, 0);

    let (_, program_config) = test_setup.fetch_program_config().await;

    let mut expected_program_config = ProgramConfig::default();
    expected_program_config.admin_key = admin_signer.pubkey();
    expected_program_config.next_dz_epoch = DoubleZeroEpoch::new(1);
    expected_program_config.accountant_key = accountant_signer.pubkey();
    expected_program_config.current_solana_validator_fee =
        ValidatorFee::new(solana_validator_fee).unwrap();
    expected_program_config.community_burn_rate_parameters = cbr_params;
    assert_eq!(program_config, expected_program_config);

    // Create another distribution.

    test_setup
        .initialize_distribution(&accountant_signer)
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
    let (_, distribution, distribution_custody) = test_setup.fetch_distribution(dz_epoch).await;

    let mut expected_distribution = Distribution::default();
    expected_distribution.dz_epoch = dz_epoch;
    expected_distribution.community_burn_rate = expected_cbr;
    assert_eq!(distribution, expected_distribution);
    assert_eq!(distribution_custody.amount, 0);

    let (_, program_config) = test_setup.fetch_program_config().await;

    let mut expected_program_config = ProgramConfig::default();
    expected_program_config.admin_key = admin_signer.pubkey();
    expected_program_config.next_dz_epoch = DoubleZeroEpoch::new(2);
    expected_program_config.accountant_key = accountant_signer.pubkey();
    expected_program_config.current_solana_validator_fee =
        ValidatorFee::new(solana_validator_fee).unwrap();
    expected_program_config.community_burn_rate_parameters = cbr_params;
    assert_eq!(program_config, expected_program_config);
}

//
// Configure distribution.
//

#[tokio::test]
async fn test_configure_distribution() {
    let mut test_setup = common::start_test().await;

    let admin_signer = Keypair::new();

    let accountant_signer = Keypair::new();
    let solana_validator_fee = 500; // 5%

    // Community burn rate.
    let initial_cbr = 100_000_000; // 10%
    let cbr_limit = 500_000_000; // 50%
    let dz_epochs_to_increasing_cbr = 10;
    let dz_epochs_to_cbr_limit = 20;

    // Test inputs.

    let dz_epoch = DoubleZeroEpoch::new(1);

    let total_solana_validator_payments_owed = 100_000_000_000; // 100 SOL
    let solana_validator_payments_merkle_root = Hash::new_unique();

    let total_contributors = 69;
    let contributor_rewards_merkle_root = Hash::new_unique();

    test_setup
        .initialize_program()
        .await
        .unwrap()
        .set_admin(AdminKey::new(admin_signer.pubkey()))
        .await
        .unwrap()
        .configure_program(
            [
                ConfigureProgramSetting::Accountant(accountant_signer.pubkey()),
                ConfigureProgramSetting::SolanaValidatorFee(solana_validator_fee),
                ConfigureProgramSetting::CommunityBurnRateParameters {
                    limit: cbr_limit,
                    dz_epochs_to_increasing: dz_epochs_to_increasing_cbr,
                    dz_epochs_to_limit: dz_epochs_to_cbr_limit,
                    initial_rate: Some(initial_cbr),
                },
                ConfigureProgramSetting::Flag(ConfigureFlag::IsPaused(false)),
            ],
            &admin_signer,
        )
        .await
        .unwrap()
        .initialize_distribution(&accountant_signer)
        .await
        .unwrap()
        .initialize_distribution(&accountant_signer)
        .await
        .unwrap()
        .configure_distribution(
            dz_epoch,
            [
                ConfigureDistributionData::SolanaValidatorPayments {
                    total_owed: total_solana_validator_payments_owed,
                    merkle_root: solana_validator_payments_merkle_root,
                },
                ConfigureDistributionData::ContributorRewards {
                    total_contributors,
                    merkle_root: contributor_rewards_merkle_root,
                },
            ],
            &accountant_signer,
        )
        .await
        .unwrap();

    let (_, distribution, _) = test_setup.fetch_distribution(dz_epoch).await;

    let mut expected_distribution = Distribution::default();
    expected_distribution.dz_epoch = dz_epoch;
    expected_distribution.community_burn_rate = BurnRate::new(initial_cbr).unwrap();
    expected_distribution.total_solana_validator_payments_owed =
        total_solana_validator_payments_owed;
    expected_distribution.solana_validator_payments_merkle_root =
        solana_validator_payments_merkle_root;
    expected_distribution.total_contributors = total_contributors;
    expected_distribution.contributor_rewards_merkle_root = contributor_rewards_merkle_root;
    assert_eq!(distribution, expected_distribution);
}
