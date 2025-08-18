mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::{DenyPrepaidConnectionAccessAccounts, LoadPrepaidConnectionAccounts},
        JournalConfiguration, ProgramConfiguration, ProgramFlagConfiguration,
        RevenueDistributionInstructionData,
    },
    state::{JournalEntries, JournalEntry, PrepaidConnection},
    types::DoubleZeroEpoch,
    DOUBLEZERO_MINT_KEY, ID,
};
use solana_program_test::tokio;
use solana_pubkey::Pubkey;
use solana_sdk::{
    instruction::InstructionError,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};

#[tokio::test]
async fn test_load_prepaid_connection() {
    let transfer_authority_signer = Keypair::new();

    let bootstrapped_accounts = common::generate_token_accounts_for_test(
        &DOUBLEZERO_MINT_KEY,
        &[transfer_authority_signer.pubkey()],
    );
    let src_token_account_key = bootstrapped_accounts.first().unwrap().key;

    let mut test_setup = common::start_test_with_accounts(bootstrapped_accounts).await;

    let admin_signer = Keypair::new();
    let dz_ledger_sentinel_signer = Keypair::new();

    // Prepaid connection settings.
    let prepaid_activation_cost = 20_000;
    let prepaid_cost_per_dz_epoch = 10_000;

    let prepaid_minimum_prepaid_dz_epochs = 1;
    let prepaid_maximum_entries = 10;

    let user_1_key = Pubkey::new_unique();
    let user_2_key = Pubkey::new_unique();
    let user_3_key = Pubkey::new_unique();

    test_setup
        .transfer_2z(&src_token_account_key, 1_000_000 * u64::pow(10, 8))
        .await
        .unwrap()
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
                ProgramConfiguration::DoubleZeroLedgerSentinel(dz_ledger_sentinel_signer.pubkey()),
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
            ],
        )
        .await
        .unwrap()
        .configure_journal(
            &admin_signer,
            [
                JournalConfiguration::ActivationCost(prepaid_activation_cost),
                JournalConfiguration::CostPerDoubleZeroEpoch(prepaid_cost_per_dz_epoch),
                JournalConfiguration::EntryBoundaries {
                    minimum_prepaid_dz_epochs: prepaid_minimum_prepaid_dz_epochs,
                    maximum_entries: prepaid_maximum_entries,
                },
            ],
        )
        .await
        .unwrap();

    for user_key in &[user_1_key, user_2_key, user_3_key] {
        test_setup
            .initialize_prepaid_connection(
                user_key,
                &transfer_authority_signer,
                &src_token_account_key,
                8,
            )
            .await
            .unwrap();
    }

    // Test input.

    let valid_through_dz_epoch = DoubleZeroEpoch::new(5);

    // Cannot load a prepaid connection that does not have access.
    let load_prepaid_connection_access_ix = try_build_instruction(
        &ID,
        LoadPrepaidConnectionAccounts::new(
            &src_token_account_key,
            &DOUBLEZERO_MINT_KEY,
            &transfer_authority_signer.pubkey(),
            &user_1_key,
        ),
        &RevenueDistributionInstructionData::LoadPrepaidConnection {
            valid_through_dz_epoch,
            decimals: 8,
        },
    )
    .unwrap();

    let (tx_err, program_logs) = test_setup
        .unwrap_simulation_error(
            &[load_prepaid_connection_access_ix],
            &[&transfer_authority_signer],
        )
        .await;
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(2).unwrap(),
        "Program log: Prepaid connection does not have access to DoubleZero Ledger"
    );

    // Grant access to all prepaid connections.
    for user_key in &[user_1_key, user_2_key, user_3_key] {
        test_setup
            .grant_prepaid_connection_access(&dz_ledger_sentinel_signer, user_key)
            .await
            .unwrap();
    }

    let starting_src_balance = test_setup
        .fetch_token_account(&src_token_account_key)
        .await
        .unwrap()
        .amount;

    test_setup
        .load_prepaid_connection(
            &user_1_key,
            &transfer_authority_signer,
            &src_token_account_key,
            valid_through_dz_epoch,
            8,
        )
        .await
        .unwrap();

    let ending_src_balance = test_setup
        .fetch_token_account(&src_token_account_key)
        .await
        .unwrap()
        .amount;

    // Compute the total cost. Because global DZ epoch is 0, we needed to have paid for 6 epochs.
    let expected_total_payment = 6 * u64::from(prepaid_cost_per_dz_epoch) * u64::pow(10, 8);
    assert_eq!(
        starting_src_balance - ending_src_balance,
        expected_total_payment
    );

    let expected_activation_cost = u64::from(prepaid_activation_cost) * u64::pow(10, 8);

    let (_, prepaid_connection) = test_setup.fetch_prepaid_connection(&user_1_key).await;

    let mut expected_prepaid_connection_1 = PrepaidConnection::default();
    expected_prepaid_connection_1.user_key = user_1_key;
    expected_prepaid_connection_1.set_has_access_granted(true);
    expected_prepaid_connection_1.set_has_paid(true);
    expected_prepaid_connection_1.valid_through_dz_epoch = valid_through_dz_epoch;
    expected_prepaid_connection_1.termination_beneficiary_key = test_setup.payer_signer.pubkey();
    expected_prepaid_connection_1.activation_cost = expected_activation_cost;
    expected_prepaid_connection_1.activation_funder_key = src_token_account_key;
    assert_eq!(prepaid_connection, expected_prepaid_connection_1);

    let (_, journal, journal_entries, journal_2z_pda) = test_setup.fetch_journal().await;

    let total_journal_balance = expected_total_payment;
    assert_eq!(journal.total_2z_balance, total_journal_balance);
    assert_eq!(journal_2z_pda.amount, total_journal_balance);

    let expected_journal_entries = JournalEntries(
        vec![
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(0),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(1),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(2),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(3),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(4),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(5),
                amount: prepaid_cost_per_dz_epoch,
            },
        ]
        .into(),
    );
    assert_eq!(journal_entries, expected_journal_entries);

    // Load again.

    let starting_src_balance = ending_src_balance;
    let last_journal_balance = total_journal_balance;

    let valid_through_dz_epoch = DoubleZeroEpoch::new(7);

    test_setup
        .load_prepaid_connection(
            &user_1_key,
            &transfer_authority_signer,
            &src_token_account_key,
            valid_through_dz_epoch,
            8,
        )
        .await
        .unwrap();

    let ending_src_balance = test_setup
        .fetch_token_account(&src_token_account_key)
        .await
        .unwrap()
        .amount;

    // Compute the total cost. Because we have already paid through DZ epoch 5, we needed to have
    // paid for 2 more epochs.
    let expected_total_payment = 2 * u64::from(prepaid_cost_per_dz_epoch) * u64::pow(10, 8);
    assert_eq!(
        starting_src_balance - ending_src_balance,
        expected_total_payment
    );

    let (_, prepaid_connection) = test_setup.fetch_prepaid_connection(&user_1_key).await;
    expected_prepaid_connection_1.valid_through_dz_epoch = valid_through_dz_epoch;
    assert_eq!(prepaid_connection, expected_prepaid_connection_1);

    let (_, journal, journal_entries, journal_2z_pda) = test_setup.fetch_journal().await;

    let total_journal_balance = last_journal_balance + expected_total_payment;
    assert_eq!(journal.total_2z_balance, total_journal_balance);
    assert_eq!(journal_2z_pda.amount, total_journal_balance);

    let expected_journal_entries = JournalEntries(
        vec![
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(0),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(1),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(2),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(3),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(4),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(5),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(6),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(7),
                amount: prepaid_cost_per_dz_epoch,
            },
        ]
        .into(),
    );
    assert_eq!(journal_entries, expected_journal_entries);

    // Load another user.

    let starting_src_balance = ending_src_balance;
    let last_journal_balance = total_journal_balance;

    let valid_through_dz_epoch = DoubleZeroEpoch::new(3);

    test_setup
        .load_prepaid_connection(
            &user_2_key,
            &transfer_authority_signer,
            &src_token_account_key,
            valid_through_dz_epoch,
            8,
        )
        .await
        .unwrap();

    let ending_src_balance = test_setup
        .fetch_token_account(&src_token_account_key)
        .await
        .unwrap()
        .amount;

    // Compute the total cost. Because global DZ epoch is 0, we needed to have paid for 4 epochs.
    let expected_total_payment = 4 * u64::from(prepaid_cost_per_dz_epoch) * u64::pow(10, 8);
    assert_eq!(
        starting_src_balance - ending_src_balance,
        expected_total_payment
    );

    let (_, prepaid_connection) = test_setup.fetch_prepaid_connection(&user_2_key).await;

    let mut expected_prepaid_connection_2 = PrepaidConnection::default();
    expected_prepaid_connection_2.user_key = user_2_key;
    expected_prepaid_connection_2.set_has_access_granted(true);
    expected_prepaid_connection_2.set_has_paid(true);
    expected_prepaid_connection_2.valid_through_dz_epoch = valid_through_dz_epoch;
    expected_prepaid_connection_2.termination_beneficiary_key = test_setup.payer_signer.pubkey();
    expected_prepaid_connection_2.activation_cost = expected_activation_cost;
    expected_prepaid_connection_2.activation_funder_key = src_token_account_key;
    assert_eq!(prepaid_connection, expected_prepaid_connection_2);

    let (_, journal, journal_entries, journal_2z_pda) = test_setup.fetch_journal().await;

    let total_journal_balance = last_journal_balance + expected_total_payment;
    assert_eq!(journal.total_2z_balance, total_journal_balance);
    assert_eq!(journal_2z_pda.amount, total_journal_balance);

    let expected_journal_entries = JournalEntries(
        vec![
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(0),
                amount: 2 * prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(1),
                amount: 2 * prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(2),
                amount: 2 * prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(3),
                amount: 2 * prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(4),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(5),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(6),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(7),
                amount: prepaid_cost_per_dz_epoch,
            },
        ]
        .into(),
    );
    assert_eq!(journal_entries, expected_journal_entries);

    // Initialize new distribution.

    let last_journal_balance = total_journal_balance;

    let payments_accountant_signer = Keypair::new();
    let solana_validator_base_block_rewards_fee = 500; // 5%.

    // Community burn rate.
    let initial_cbr = 100_000_000; // 10%.
    let cbr_limit = 500_000_000; // 50%.
    let dz_epochs_to_increasing_cbr = 10;
    let dz_epochs_to_cbr_limit = 20;

    test_setup
        .configure_program(
            &admin_signer,
            [
                ProgramConfiguration::PaymentsAccountant(payments_accountant_signer.pubkey()),
                ProgramConfiguration::SolanaValidatorFeeParameters {
                    base_block_rewards: solana_validator_base_block_rewards_fee,
                    priority_block_rewards: 0,
                    inflation_rewards: 0,
                    jito_tips: 0,
                    _unused: [0; 32],
                },
                ProgramConfiguration::CommunityBurnRateParameters {
                    limit: cbr_limit,
                    dz_epochs_to_increasing: dz_epochs_to_increasing_cbr,
                    dz_epochs_to_limit: dz_epochs_to_cbr_limit,
                    initial_rate: Some(initial_cbr),
                },
                ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(false)),
            ],
        )
        .await
        .unwrap()
        .initialize_distribution(&payments_accountant_signer)
        .await
        .unwrap();

    let expected_journal_entry_amount = 2 * prepaid_cost_per_dz_epoch;
    assert_eq!(
        journal_entries.front_entry().unwrap().amount,
        expected_journal_entry_amount
    );

    let expected_transfer_amount = u64::from(expected_journal_entry_amount) * u64::pow(10, 8);

    let (_, _, _, _, distribution_2z_token_pda) =
        test_setup.fetch_distribution(DoubleZeroEpoch::new(0)).await;
    assert_eq!(distribution_2z_token_pda.amount, expected_transfer_amount);

    let (_, journal, journal_entries, journal_2z_pda) = test_setup.fetch_journal().await;

    // The balance on the journal should change by the first entry's amount.

    let total_journal_balance = last_journal_balance - expected_transfer_amount;
    assert_eq!(journal.total_2z_balance, total_journal_balance);
    assert_eq!(journal_2z_pda.amount, total_journal_balance);

    let expected_journal_entries = JournalEntries(
        vec![
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(1),
                amount: 2 * prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(2),
                amount: 2 * prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(3),
                amount: 2 * prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(4),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(5),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(6),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(7),
                amount: prepaid_cost_per_dz_epoch,
            },
        ]
        .into(),
    );
    assert_eq!(journal_entries, expected_journal_entries);

    // Load another user and initialize another distribution.

    let starting_src_balance = ending_src_balance;
    let last_journal_balance = total_journal_balance;

    // NOTE: This should be the maximum a user can load.
    let valid_through_dz_epoch = DoubleZeroEpoch::new(11);

    test_setup
        .load_prepaid_connection(
            &user_3_key,
            &transfer_authority_signer,
            &src_token_account_key,
            valid_through_dz_epoch,
            8,
        )
        .await
        .unwrap()
        .initialize_distribution(&payments_accountant_signer)
        .await
        .unwrap();

    let ending_src_balance = test_setup
        .fetch_token_account(&src_token_account_key)
        .await
        .unwrap()
        .amount;

    // Compute the total cost. Because global DZ epoch is 1, we needed to have paid for 11 epochs.
    let expected_total_payment = 11 * u64::from(prepaid_cost_per_dz_epoch) * u64::pow(10, 8);
    assert_eq!(
        starting_src_balance - ending_src_balance,
        expected_total_payment
    );

    let (_, prepaid_connection) = test_setup.fetch_prepaid_connection(&user_3_key).await;

    let mut expected_prepaid_connection_3 = PrepaidConnection::default();
    expected_prepaid_connection_3.user_key = user_3_key;
    expected_prepaid_connection_3.set_has_access_granted(true);
    expected_prepaid_connection_3.set_has_paid(true);
    expected_prepaid_connection_3.valid_through_dz_epoch = valid_through_dz_epoch;
    expected_prepaid_connection_3.termination_beneficiary_key = test_setup.payer_signer.pubkey();
    expected_prepaid_connection_3.activation_cost = expected_activation_cost;
    expected_prepaid_connection_3.activation_funder_key = src_token_account_key;
    assert_eq!(prepaid_connection, expected_prepaid_connection_3);

    let expected_journal_entry_amount = 3 * prepaid_cost_per_dz_epoch;
    assert_eq!(
        journal_entries.front_entry().unwrap().amount + prepaid_cost_per_dz_epoch,
        expected_journal_entry_amount
    );

    let expected_transfer_amount = u64::from(expected_journal_entry_amount) * u64::pow(10, 8);

    let (_, _, _, _, distribution_2z_token_pda) =
        test_setup.fetch_distribution(DoubleZeroEpoch::new(1)).await;
    assert_eq!(distribution_2z_token_pda.amount, expected_transfer_amount);

    let (_, journal, journal_entries, journal_2z_pda) = test_setup.fetch_journal().await;

    // The balance on the journal should change by the first entry's amount.

    let total_journal_balance =
        last_journal_balance + expected_total_payment - expected_transfer_amount;
    assert_eq!(journal.total_2z_balance, total_journal_balance);
    assert_eq!(journal_2z_pda.amount, total_journal_balance);

    let expected_journal_entries = JournalEntries(
        vec![
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(2),
                amount: 3 * prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(3),
                amount: 3 * prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(4),
                amount: 2 * prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(5),
                amount: 2 * prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(6),
                amount: 2 * prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(7),
                amount: 2 * prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(8),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(9),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(10),
                amount: prepaid_cost_per_dz_epoch,
            },
            JournalEntry {
                dz_epoch: DoubleZeroEpoch::new(11),
                amount: prepaid_cost_per_dz_epoch,
            },
        ]
        .into(),
    );
    assert_eq!(journal_entries, expected_journal_entries);

    // Cannot deny access to any of the prepaid connections that already have
    // been loaded.
    for user_key in &[user_1_key, user_2_key, user_3_key] {
        let deny_prepaid_connection_access_ix = try_build_instruction(
            &ID,
            DenyPrepaidConnectionAccessAccounts::new(
                &dz_ledger_sentinel_signer.pubkey(),
                &Pubkey::new_unique(),
                &Pubkey::new_unique(),
                user_key,
            ),
            &RevenueDistributionInstructionData::DenyPrepaidConnectionAccess,
        )
        .unwrap();

        let (tx_err, program_logs) = test_setup
            .unwrap_simulation_error(
                &[deny_prepaid_connection_access_ix],
                &[&dz_ledger_sentinel_signer],
            )
            .await;
        assert_eq!(
            tx_err,
            TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
        );
        assert_eq!(
            program_logs.get(2).unwrap(),
            "Program log: Prepaid connection already has access"
        );
    }
}
