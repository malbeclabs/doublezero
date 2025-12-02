mod common;

//

use doublezero_revenue_distribution::{
    state::{find_2z_token_pda_address, find_swap_authority_address},
    DOUBLEZERO_MINT_KEY,
};
use solana_program_pack::Pack;
use solana_program_test::tokio;
use spl_token_interface::state::{Account as TokenAccount, AccountState as SplTokenAccountState};

//
// Initialize swap destination.
//

#[tokio::test]
async fn test_initialize_swap_destination() {
    let mut test_setup = common::start_test().await;

    test_setup
        .initialize_program()
        .await
        .unwrap()
        .initialize_swap_destination(&DOUBLEZERO_MINT_KEY)
        .await
        .unwrap();

    let (swap_authority_key, swap_authority_bump) = find_swap_authority_address();
    let (swap_destination_key, swap_dst_2z_token_pda_bump) =
        find_2z_token_pda_address(&swap_authority_key);
    let swap_destination_account_data = test_setup
        .context
        .banks_client
        .get_account(swap_destination_key)
        .await
        .unwrap()
        .unwrap()
        .data;

    let swap_destination_token_account =
        TokenAccount::unpack(&swap_destination_account_data).unwrap();
    let expected_swap_destination_token_account = TokenAccount {
        mint: DOUBLEZERO_MINT_KEY,
        owner: swap_authority_key,
        state: SplTokenAccountState::Initialized,
        ..Default::default()
    };
    assert_eq!(
        swap_destination_token_account,
        expected_swap_destination_token_account
    );

    // Just check the new bump seeds.
    let (_, program_config, _) = test_setup.fetch_program_config().await;
    assert_eq!(program_config.swap_authority_bump_seed, swap_authority_bump);
    assert_eq!(
        program_config.swap_destination_2z_bump_seed,
        swap_dst_2z_token_pda_bump
    );
}
