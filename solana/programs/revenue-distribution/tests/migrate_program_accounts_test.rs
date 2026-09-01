mod common;

//

use doublezero_program_tools::{
    instruction::try_build_instruction, zero_copy, PrecomputedDiscriminator, DISCRIMINATOR_LEN,
};
use doublezero_revenue_distribution::{
    instruction::{account::MigrateProgramAccountsAccounts, RevenueDistributionInstructionData},
    state::{self, Distribution},
    types::DoubleZeroEpoch,
    DOUBLEZERO_MINT_KEY, ID,
};
use solana_program_pack::Pack;
use solana_program_test::tokio;
use solana_sdk::{
    account::Account,
    instruction::InstructionError,
    rent::Rent,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};
use spl_token_interface::state::{Account as TokenAccount, AccountState as SplTokenAccountState};

//
// Setup.
//

struct MigrateProgramAccountsSetup {
    test_setup: common::ProgramTestWithOwner,
    journal_integrations_count: u16,
}

async fn setup_for_migrate_program_accounts() -> MigrateProgramAccountsSetup {
    let mut test_setup = common::start_test().await;
    let configured = test_setup.setup_configured_program().await.unwrap();

    // Register one integration so the journal's integrations_count is non-zero
    // and the migration's effect is observable.
    let integration_program_id = mock_swap_sol_2z::ID;
    test_setup
        .initialize_rewards_integration(&configured.admin_signer, &integration_program_id)
        .await
        .unwrap();

    let (_, journal, _) = test_setup.fetch_journal().await;

    MigrateProgramAccountsSetup {
        test_setup,
        journal_integrations_count: journal.integrations_count,
    }
}

async fn migrate_program_accounts(
    test_setup: &mut common::ProgramTestWithOwner,
    dz_epochs: &[DoubleZeroEpoch],
) {
    let owner_signer = &test_setup.owner_signer;
    let payer_signer = &test_setup.context.payer;

    let migrate_ix = try_build_instruction(
        &ID,
        MigrateProgramAccountsAccounts::new(&ID, &owner_signer.pubkey(), dz_epochs),
        &RevenueDistributionInstructionData::MigrateProgramAccounts,
    )
    .unwrap();

    test_setup.context.last_blockhash = common::process_instructions_for_test(
        &mut test_setup.context.banks_client,
        &test_setup.context.last_blockhash,
        &[migrate_ix],
        &[payer_signer, owner_signer],
    )
    .await
    .unwrap();
}

/// Inject a `Distribution` account at the canonical PDA for `dz_epoch`,
/// simulating one initialized before `integrations_count_snapshot` was wired
/// up correctly (snapshot stays at zero). The matching 2Z token PDA is also
/// seeded so `fetch_distribution` can read both sides.
fn inject_distribution(test_setup: &mut common::ProgramTestWithOwner, dz_epoch: DoubleZeroEpoch) {
    let (key, bump_seed) = Distribution::find_address(dz_epoch);
    let (token_pda_key, token_2z_pda_bump_seed) = state::find_2z_token_pda_address(&key);

    let data_len = zero_copy::data_end::<Distribution>();
    let mut data = vec![0; data_len];
    data[..DISCRIMINATOR_LEN].copy_from_slice(Distribution::discriminator_slice());

    let distribution = bytemuck::from_bytes_mut::<Distribution>(
        &mut data[zero_copy::data_range::<Distribution>()],
    );
    distribution.dz_epoch = dz_epoch;
    distribution.bump_seed = bump_seed;
    distribution.token_2z_pda_bump_seed = token_2z_pda_bump_seed;

    let rent = Rent::default();
    let distribution_account = Account {
        lamports: rent.minimum_balance(data_len),
        data,
        owner: ID,
        executable: false,
        rent_epoch: 0,
    };

    let token_pda = TokenAccount {
        mint: DOUBLEZERO_MINT_KEY,
        owner: key,
        state: SplTokenAccountState::Initialized,
        ..Default::default()
    };
    let mut token_pda_data = vec![0; TokenAccount::LEN];
    token_pda.pack_into_slice(&mut token_pda_data);
    let token_pda_account = Account {
        lamports: rent.minimum_balance(TokenAccount::LEN),
        data: token_pda_data,
        owner: spl_token_interface::ID,
        executable: false,
        rent_epoch: 0,
    };

    test_setup
        .context
        .set_account(&key, &distribution_account.into());
    test_setup
        .context
        .set_account(&token_pda_key, &token_pda_account.into());
}

//
// Migrate program accounts — happy path.
//

#[tokio::test]
async fn test_migrate_program_accounts() {
    let MigrateProgramAccountsSetup {
        mut test_setup,
        journal_integrations_count,
    } = setup_for_migrate_program_accounts().await;

    let dz_epochs = [DoubleZeroEpoch::new(140), DoubleZeroEpoch::new(141)];
    for dz_epoch in dz_epochs {
        inject_distribution(&mut test_setup, dz_epoch);
    }

    migrate_program_accounts(&mut test_setup, &dz_epochs).await;

    for dz_epoch in dz_epochs {
        let (distribution_key, distribution, _, _, _) =
            test_setup.fetch_distribution(dz_epoch).await;

        let mut expected = Distribution::default();
        expected.dz_epoch = dz_epoch;
        expected.bump_seed = Distribution::find_address(dz_epoch).1;
        expected.token_2z_pda_bump_seed = state::find_2z_token_pda_address(&distribution_key).1;
        expected.integrations_count_snapshot = journal_integrations_count;
        assert_eq!(distribution, expected);
    }
}

//
// Migrate program accounts — reverts when any distribution is below the
// MIN_DZ_EPOCH floor.
//

#[tokio::test]
async fn test_cannot_migrate_program_accounts_below_min_dz_epoch() {
    let MigrateProgramAccountsSetup { mut test_setup, .. } =
        setup_for_migrate_program_accounts().await;

    let dz_epochs = [DoubleZeroEpoch::new(140), DoubleZeroEpoch::new(139)];
    for dz_epoch in dz_epochs {
        inject_distribution(&mut test_setup, dz_epoch);
    }

    let owner_signer = test_setup.owner_signer.insecure_clone();
    let migrate_ix = try_build_instruction(
        &ID,
        MigrateProgramAccountsAccounts::new(&ID, &owner_signer.pubkey(), &dz_epochs),
        &RevenueDistributionInstructionData::MigrateProgramAccounts,
    )
    .unwrap();

    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(&[migrate_ix], &[&owner_signer])
        .await
        .unwrap();

    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(2).unwrap(),
        "Program log: DZ epoch 139 is below migration floor 140"
    );
}

//
// Migrate program accounts — reverts when the signer is not the upgrade
// authority.
//

#[tokio::test]
async fn test_cannot_migrate_program_accounts_with_wrong_signer() {
    let MigrateProgramAccountsSetup { mut test_setup, .. } =
        setup_for_migrate_program_accounts().await;

    let impostor_signer = Keypair::new();
    let migrate_ix = try_build_instruction(
        &ID,
        MigrateProgramAccountsAccounts::new(&ID, &impostor_signer.pubkey(), &[]),
        &RevenueDistributionInstructionData::MigrateProgramAccounts,
    )
    .unwrap();

    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(&[migrate_ix], &[&impostor_signer])
        .await
        .unwrap();

    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(2).unwrap(),
        "Program log: Owner (account 1) must match upgrade authority from program data (account 0)"
    );
}
