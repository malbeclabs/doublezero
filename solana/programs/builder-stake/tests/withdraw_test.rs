mod common;

//

use doublezero_builder_stake::state::BuilderStake;
use solana_program_test::tokio;
use solana_sdk::{
    instruction::InstructionError, program_error::ProgramError, pubkey::Pubkey, signature::Signer,
};

const ONE_GBPS: u64 = 1_000_000_000;

/// Set up a funded, over-funded stake and return the pieces a withdrawal needs.
async fn staked(extra: u64) -> (common::TestSetup, Pubkey, Pubkey, Pubkey) {
    let mut t = common::start_test().await;
    t.initialize_and_unpause().await;

    let builder = t.builder.insecure_clone();
    let builder_key = builder.pubkey();
    let source_token_account = t.builder_2z_key;

    t.send(
        common::initialize_builder_stake(&builder_key, 0, ONE_GBPS),
        &[&builder],
    )
    .await
    .unwrap();
    t.send(
        common::post_bond(
            &builder_key,
            0,
            &source_token_account,
            common::TIER_1GBPS + extra,
        ),
        &[&builder],
    )
    .await
    .unwrap();

    let stake_key = BuilderStake::find_address(&builder_key, 0).0;
    (t, builder_key, stake_key, source_token_account)
}

/// The first bond starts the hold, and a later one does not restart it. A repricing can force a
/// top-up, and that must not push the builder's withdrawal date out.
#[tokio::test]
async fn test_first_bond_starts_the_hold_and_later_ones_do_not_restart_it() {
    let (mut t, builder_key, stake_key, source_token_account) = staked(500).await;
    let builder = t.builder.insecure_clone();

    let started = t.read_builder_stake(&stake_key).await.hold_expires_at;
    assert!(started > 0, "the first bond sets the hold");

    t.send(
        common::post_bond(&builder_key, 0, &source_token_account, 500),
        &[&builder],
    )
    .await
    .unwrap();

    assert_eq!(
        t.read_builder_stake(&stake_key).await.hold_expires_at,
        started,
        "a top-up must not move the hold"
    );
}

/// Nothing comes out during the hold, however much the stake holds above its requirement.
#[tokio::test]
async fn test_nothing_is_withdrawable_during_the_hold() {
    let (mut t, builder_key, stake_key, destination) = staked(500).await;
    let builder = t.builder.insecure_clone();

    let stake = t.read_builder_stake(&stake_key).await;
    assert_eq!(stake.bonded_2z_amount, common::TIER_1GBPS + 500);
    assert_eq!(stake.withdrawable_2z_amount(0), 0);

    let err = t
        .send(
            common::withdraw(&builder_key, 0, &destination, 1),
            &[&builder],
        )
        .await
        .expect_err("the hold has not elapsed");
    common::assert_instruction_error(
        err,
        InstructionError::from(u64::from(ProgramError::InvalidAccountData)),
    );
}

/// Once the hold elapses the excess comes out and the requirement stays behind.
#[tokio::test]
async fn test_the_excess_comes_out_after_the_hold() {
    let (mut t, builder_key, stake_key, destination) = staked(500).await;
    let builder = t.builder.insecure_clone();
    let admin = t.upgrade_authority.pubkey();
    let upgrade_authority = t.upgrade_authority.insecure_clone();

    // Stand where the demo stands six months from now.
    t.send(
        common::set_hold_expiry(&admin, &builder_key, 0, 1),
        &[&upgrade_authority],
    )
    .await
    .unwrap();

    let before = t.token_amount(&destination).await;

    t.send(
        common::withdraw(&builder_key, 0, &destination, 500),
        &[&builder],
    )
    .await
    .unwrap();

    let stake = t.read_builder_stake(&stake_key).await;
    assert_eq!(stake.bonded_2z_amount, common::TIER_1GBPS);
    assert!(stake.is_funded(), "the requirement stays behind");
    assert_eq!(t.token_amount(&destination).await, before + 500);

    // And now there is nothing spare left.
    let err = t
        .send(
            common::withdraw(&builder_key, 0, &destination, 1),
            &[&builder],
        )
        .await
        .expect_err("a funded stake has no excess");
    common::assert_instruction_error(
        err,
        InstructionError::from(u64::from(ProgramError::InsufficientFunds)),
    );
}

/// The requirement is a floor even after the hold. This is what stops a builder walking its bond
/// out from under a live feed.
#[tokio::test]
async fn test_the_requirement_cannot_be_withdrawn() {
    let (mut t, builder_key, stake_key, destination) = staked(500).await;
    let builder = t.builder.insecure_clone();
    let admin = t.upgrade_authority.pubkey();
    let upgrade_authority = t.upgrade_authority.insecure_clone();

    t.send(
        common::set_hold_expiry(&admin, &builder_key, 0, 1),
        &[&upgrade_authority],
    )
    .await
    .unwrap();

    // One unit more than the excess.
    let err = t
        .send(
            common::withdraw(&builder_key, 0, &destination, 501),
            &[&builder],
        )
        .await
        .expect_err("the requirement is not withdrawable");
    common::assert_instruction_error(
        err,
        InstructionError::from(u64::from(ProgramError::InsufficientFunds)),
    );

    let stake = t.read_builder_stake(&stake_key).await;
    assert_eq!(stake.bonded_2z_amount, common::TIER_1GBPS + 500);
}

/// A stranger cannot withdraw another builder's bond, even to their own token account.
#[tokio::test]
async fn test_only_the_builder_can_withdraw() {
    let (mut t, builder_key, _, destination) = staked(500).await;
    let admin = t.upgrade_authority.pubkey();
    let upgrade_authority = t.upgrade_authority.insecure_clone();

    t.send(
        common::set_hold_expiry(&admin, &builder_key, 0, 1),
        &[&upgrade_authority],
    )
    .await
    .unwrap();

    // The payer signs in the builder's place, pointing at the builder's stake.
    let payer = t.context.payer.insecure_clone();
    let mut ix = common::withdraw(&builder_key, 0, &destination, 500);
    ix.accounts[1] = solana_sdk::instruction::AccountMeta::new_readonly(payer.pubkey(), true);

    let err = t
        .send(ix, &[])
        .await
        .expect_err("only the stake's builder may withdraw");
    common::assert_instruction_error(
        err,
        InstructionError::from(u64::from(ProgramError::IncorrectAuthority)),
    );
}

/// Only the admin can move a hold, even in a development build.
#[tokio::test]
async fn test_only_the_admin_can_move_the_hold() {
    let (mut t, builder_key, stake_key, _) = staked(500).await;
    let builder = t.builder.insecure_clone();

    let before = t.read_builder_stake(&stake_key).await.hold_expires_at;

    let err = t
        .send(
            common::set_hold_expiry(&builder.pubkey(), &builder_key, 0, 1),
            &[&builder],
        )
        .await
        .expect_err("a builder cannot move its own hold");
    common::assert_instruction_error(
        err,
        InstructionError::from(u64::from(ProgramError::IncorrectAuthority)),
    );

    assert_eq!(
        t.read_builder_stake(&stake_key).await.hold_expires_at,
        before
    );
}
