mod common;

//

use doublezero_builder_stake::state::{self, BuilderStake, ProgramConfig};
use solana_program_test::tokio;
use solana_sdk::signature::Signer;

const ONE_GBPS: u64 = 1_000_000_000;

/// A new deployment has no admin and no tier table, so it must not take deposits until an operator
/// configures it.
#[tokio::test]
async fn test_initialize_program_starts_paused() {
    let mut t = common::start_test().await;
    let payer = t.context.payer.pubkey();

    t.send(common::initialize_program(&payer), &[])
        .await
        .unwrap();

    let config = t.read_program_config().await;
    assert!(config.is_paused());
    assert_eq!(config.admin_key, Default::default());
    assert_eq!(config.bump_seed, ProgramConfig::find_address().1);
}

/// The upgrade authority sets the admin, not the admin itself. A lost admin key is recoverable
/// through the deploy authority rather than not at all.
#[tokio::test]
async fn test_admin_is_set_by_the_upgrade_authority() {
    let mut t = common::start_test().await;
    let payer = t.context.payer.pubkey();
    t.send(common::initialize_program(&payer), &[])
        .await
        .unwrap();

    let admin = t.upgrade_authority.pubkey();
    let upgrade_authority = t.upgrade_authority.insecure_clone();

    // A stranger cannot claim it.
    let stranger = t.builder.insecure_clone();
    t.send(common::set_admin(&stranger.pubkey()), &[&stranger])
        .await
        .expect_err("a non-upgrade-authority signer should not set the admin");

    t.send(common::set_admin(&admin), &[&upgrade_authority])
        .await
        .unwrap();
    assert_eq!(t.read_program_config().await.admin_key, admin);
}

/// A builder deposits 2Z against a committed rate and the stake records what it holds.
#[tokio::test]
async fn test_deposit_moves_2z_into_the_stake() {
    let mut t = common::start_test().await;
    t.initialize_and_unpause().await;

    let builder = t.builder.insecure_clone();
    let builder_key = builder.pubkey();
    let source = t.builder_2z_key;

    t.send(
        common::initialize_builder_stake(&builder_key, 0, ONE_GBPS),
        &[&builder],
    )
    .await
    .unwrap();

    let (stake_key, stake_bump) = BuilderStake::find_address(&builder_key, 0);
    let (vault_key, vault_bump) = state::find_2z_token_pda_address(&stake_key);

    let stake = t.read_builder_stake(&stake_key).await;
    assert_eq!(stake.builder, builder_key);
    assert_eq!(stake.stake_index, 0);
    assert_eq!(stake.committed_rate_bits_per_sec, ONE_GBPS);
    assert_eq!(stake.deposited_2z_amount, 0);
    assert_eq!(stake.bump_seed, stake_bump);
    assert_eq!(stake.token_account_bump_seed, vault_bump);

    let amount = 100_000;
    t.send(
        common::deposit(&builder_key, 0, &source, amount),
        &[&builder],
    )
    .await
    .unwrap();

    assert_eq!(t.token_amount(&vault_key).await, amount);
    assert_eq!(
        t.read_builder_stake(&stake_key).await.deposited_2z_amount,
        amount
    );

    // A second deposit tops the same stake up rather than starting over.
    t.send(
        common::deposit(&builder_key, 0, &source, amount),
        &[&builder],
    )
    .await
    .unwrap();
    assert_eq!(
        t.read_builder_stake(&stake_key).await.deposited_2z_amount,
        amount * 2
    );

    // The 2Z left the builder's own account.
    assert_eq!(
        t.token_amount(&source).await,
        common::TEST_2Z_SUPPLY - amount * 2
    );
}

/// RFC-28 collateralizes each feed on its own deposit, so a builder holds one stake per feed and
/// the stakes are distinct accounts with distinct vaults.
#[tokio::test]
async fn test_a_builder_holds_one_stake_per_feed() {
    let mut t = common::start_test().await;
    t.initialize_and_unpause().await;

    let builder = t.builder.insecure_clone();
    let builder_key = builder.pubkey();

    for index in 0..2u64 {
        t.send(
            common::initialize_builder_stake(&builder_key, index, ONE_GBPS),
            &[&builder],
        )
        .await
        .unwrap();
    }

    let first = BuilderStake::find_address(&builder_key, 0).0;
    let second = BuilderStake::find_address(&builder_key, 1).0;
    assert_ne!(first, second);
    assert_ne!(
        state::find_2z_token_pda_address(&first).0,
        state::find_2z_token_pda_address(&second).0
    );

    // Depositing into one leaves the other alone, which is what "slashing one never reaches
    // another" needs.
    t.send(
        common::deposit(&builder_key, 0, &t.builder_2z_key, 500),
        &[&builder],
    )
    .await
    .unwrap();
    assert_eq!(t.read_builder_stake(&first).await.deposited_2z_amount, 500);
    assert_eq!(t.read_builder_stake(&second).await.deposited_2z_amount, 0);

    // The same index twice is the same address, so it cannot be created again.
    t.send(
        common::initialize_builder_stake(&builder_key, 0, ONE_GBPS),
        &[&builder],
    )
    .await
    .expect_err("reusing a stake index should fail");
}

/// A paused program takes no deposits. This is the switch that keeps a deployed but unconfigured
/// program from holding money it has no tier table to size.
#[tokio::test]
async fn test_paused_program_takes_no_stake() {
    let mut t = common::start_test().await;
    let payer = t.context.payer.pubkey();
    t.send(common::initialize_program(&payer), &[])
        .await
        .unwrap();

    let builder = t.builder.insecure_clone();
    t.send(
        common::initialize_builder_stake(&builder.pubkey(), 0, ONE_GBPS),
        &[&builder],
    )
    .await
    .expect_err("a paused program should refuse a new stake");
}

/// A stake backing no rate backs nothing.
#[tokio::test]
async fn test_zero_committed_rate_rejected() {
    let mut t = common::start_test().await;
    t.initialize_and_unpause().await;

    let builder = t.builder.insecure_clone();
    t.send(
        common::initialize_builder_stake(&builder.pubkey(), 0, 0),
        &[&builder],
    )
    .await
    .expect_err("a zero committed rate should be refused");
}

/// A builder cannot deposit into somebody else's stake, even though the token transfer itself
/// would be signed correctly.
#[tokio::test]
async fn test_deposit_into_another_builders_stake_rejected() {
    let mut t = common::start_test().await;
    t.initialize_and_unpause().await;

    let builder = t.builder.insecure_clone();
    t.send(
        common::initialize_builder_stake(&builder.pubkey(), 0, ONE_GBPS),
        &[&builder],
    )
    .await
    .unwrap();

    // The payer signs, holding its own 2Z, and points at the builder's stake.
    let payer = t.context.payer.insecure_clone();
    let stranger_2z = solana_sdk::pubkey::Pubkey::new_unique();
    let account = common::token_account(&payer.pubkey(), 1_000);
    t.context.set_account(&stranger_2z, &account.into());

    let mut ix = common::deposit(&builder.pubkey(), 0, &stranger_2z, 500);
    // Re-point the signer at the payer while leaving the stake as the builder's.
    ix.accounts[1] = solana_sdk::instruction::AccountMeta::new_readonly(payer.pubkey(), true);

    t.send(ix, &[])
        .await
        .expect_err("depositing into another builder's stake should fail");
}
