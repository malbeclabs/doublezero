mod common;

//

use doublezero_program_tools::zero_copy::checked_from_bytes_with_discriminator;
use doublezero_revenue_distribution::{
    state::{self, Journal},
    DOUBLEZERO_MINT_KEY,
};
use solana_program_pack::Pack;
use solana_program_test::tokio;
use spl_token::state::{Account as TokenAccount, AccountState as SplTokenAccountState};

//
// Initialize journal.
//

#[tokio::test]
async fn test_initialize_journal() {
    let mut test_setup = common::start_test().await;

    test_setup.initialize_journal().await.unwrap();

    let journal_key = Journal::find_address().0;
    let journal_account_data = test_setup
        .context
        .banks_client
        .get_account(journal_key)
        .await
        .unwrap()
        .unwrap()
        .data;

    let (journal, _) =
        checked_from_bytes_with_discriminator::<Journal>(&journal_account_data).unwrap();

    let (journal_key, journal_bump) = Journal::find_address();

    let mut expected_journal = Journal::default();
    expected_journal.bump_seed = journal_bump;
    expected_journal.token_2z_pda_bump_seed = state::find_2z_token_pda_address(&journal_key).1;
    assert_eq!(journal, &expected_journal);

    let custodied_2z_token_account_data = test_setup
        .context
        .banks_client
        .get_account(state::find_2z_token_pda_address(&journal_key).0)
        .await
        .unwrap()
        .unwrap()
        .data;
    let custodied_2z_token_account =
        TokenAccount::unpack(&custodied_2z_token_account_data).unwrap();
    let expected_custodied_2z_token_account = TokenAccount {
        mint: DOUBLEZERO_MINT_KEY,
        owner: journal_key,
        state: SplTokenAccountState::Initialized,
        ..Default::default()
    };
    assert_eq!(
        custodied_2z_token_account,
        expected_custodied_2z_token_account
    );
}
