mod common;

//

use doublezero_builder_stake::state::BuilderStake;
use solana_program_test::tokio;
use solana_sdk::signature::Signer;

const ONE_GBPS: u64 = 1_000_000_000;
const FIVE_GBPS: u64 = 5_000_000_000;

/// A stake is sized by the rate it commits to, and the requirement is pinned on the account when
/// the stake is created.
#[tokio::test]
async fn test_committed_rate_picks_the_tier_amount() {
    let mut t = common::start_test().await;
    t.initialize_and_unpause().await;

    let builder = t.builder.insecure_clone();
    let builder_key = builder.pubkey();

    for (index, rate, expected) in [
        (0u64, ONE_GBPS, common::TIER_1GBPS),
        (1, ONE_GBPS + 1, common::TIER_5GBPS),
        (2, FIVE_GBPS, common::TIER_5GBPS),
        (3, FIVE_GBPS + 1, common::TIER_UNMETERED),
        (4, u64::MAX, common::TIER_UNMETERED),
    ] {
        t.send(
            common::initialize_builder_stake(&builder_key, index, rate),
            &[&builder],
        )
        .await
        .unwrap();

        let stake = t
            .read_builder_stake(&BuilderStake::find_address(&builder_key, index).0)
            .await;
        assert_eq!(
            stake.required_2z_amount, expected,
            "rate {rate} should require {expected}"
        );
        assert!(!stake.is_funded(), "a new stake holds nothing");
    }
}

/// A stake is funded once it holds its requirement, and deposits accumulate to get there.
#[tokio::test]
async fn test_stake_is_funded_once_it_holds_the_requirement() {
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
    let stake_key = BuilderStake::find_address(&builder_key, 0).0;

    // Short of the requirement. Allowed: a builder may fund in more than one transfer, and a stake
    // that is short backs no feed.
    t.send(
        common::deposit(&builder_key, 0, &source, common::TIER_1GBPS - 1),
        &[&builder],
    )
    .await
    .unwrap();
    assert!(!t.read_builder_stake(&stake_key).await.is_funded());

    // The last unit tips it over.
    t.send(common::deposit(&builder_key, 0, &source, 1), &[&builder])
        .await
        .unwrap();
    let stake = t.read_builder_stake(&stake_key).await;
    assert!(stake.is_funded());
    assert_eq!(stake.deposited_2z_amount, common::TIER_1GBPS);

    // Over-funding is allowed. RFC-28 lets a builder withdraw the excess after the hold.
    t.send(
        common::deposit(&builder_key, 0, &source, common::TIER_1GBPS),
        &[&builder],
    )
    .await
    .unwrap();
    let stake = t.read_builder_stake(&stake_key).await;
    assert!(stake.is_funded());
    assert_eq!(stake.deposited_2z_amount, common::TIER_1GBPS * 2);
}

/// An unpaused program with no tier table sizes no deposit, so it takes no stake. Without this a
/// misordered rollout would hand out stakes with a zero requirement.
#[tokio::test]
async fn test_unset_tier_table_takes_no_stake() {
    let mut t = common::start_test().await;
    t.initialize_and_set_admin().await;

    let admin = t.upgrade_authority.pubkey();
    let upgrade_authority = t.upgrade_authority.insecure_clone();
    t.send(common::set_paused(&admin, false), &[&upgrade_authority])
        .await
        .unwrap();

    let builder = t.builder.insecure_clone();
    t.send(
        common::initialize_builder_stake(&builder.pubkey(), 0, ONE_GBPS),
        &[&builder],
    )
    .await
    .expect_err("no tier table means no deposit can be sized");
}

/// A table with a hole, or one where more rate costs less, never reaches the account. A cheaper
/// high tier would have every builder buy the cheap tier and commit to the higher rate.
#[tokio::test]
async fn test_malformed_tier_table_rejected() {
    let mut t = common::start_test().await;
    t.initialize_and_set_admin().await;

    let admin = t.upgrade_authority.pubkey();
    let upgrade_authority = t.upgrade_authority.insecure_clone();

    for (a, b, c) in [
        (0, 200_000, 500_000),       // hole at the first tier
        (300_000, 200_000, 500_000), // 5 Gbps cheaper than 1 Gbps
        (100_000, 600_000, 500_000), // unmetered cheaper than 5 Gbps
    ] {
        t.send(
            common::set_tier_parameters(&admin, a, b, c),
            &[&upgrade_authority],
        )
        .await
        .unwrap_err();
    }

    // A well-formed table lands.
    t.send(
        common::set_tier_parameters(&admin, 100_000, 200_000, 500_000),
        &[&upgrade_authority],
    )
    .await
    .unwrap();
    let tiers = t.read_program_config().await.tier_parameters;
    assert_eq!(tiers.up_to_1gbps_2z_amount, 100_000);
    assert_eq!(tiers.up_to_5gbps_2z_amount, 200_000);
    assert_eq!(tiers.unmetered_2z_amount, 500_000);
}

/// Repricing a tier does not move what an existing stake owes. RFC-28 fixes the deposit at the
/// price prevailing when the tier is set, so a funded stake stays funded.
#[tokio::test]
async fn test_repricing_a_tier_leaves_existing_stakes_alone() {
    let mut t = common::start_test().await;
    t.initialize_and_unpause().await;

    let builder = t.builder.insecure_clone();
    let builder_key = builder.pubkey();

    t.send(
        common::initialize_builder_stake(&builder_key, 0, ONE_GBPS),
        &[&builder],
    )
    .await
    .unwrap();
    t.send(
        common::deposit(&builder_key, 0, &t.builder_2z_key, common::TIER_1GBPS),
        &[&builder],
    )
    .await
    .unwrap();

    let stake_key = BuilderStake::find_address(&builder_key, 0).0;
    assert!(t.read_builder_stake(&stake_key).await.is_funded());

    // The admin raises every tier tenfold.
    let admin = t.upgrade_authority.pubkey();
    let upgrade_authority = t.upgrade_authority.insecure_clone();
    t.send(
        common::set_tier_parameters(
            &admin,
            common::TIER_1GBPS * 10,
            common::TIER_5GBPS * 10,
            common::TIER_UNMETERED * 10,
        ),
        &[&upgrade_authority],
    )
    .await
    .unwrap();

    let stake = t.read_builder_stake(&stake_key).await;
    assert_eq!(stake.required_2z_amount, common::TIER_1GBPS);
    assert!(stake.is_funded());

    // A stake posted after the change pays the new price.
    t.send(
        common::initialize_builder_stake(&builder_key, 1, ONE_GBPS),
        &[&builder],
    )
    .await
    .unwrap();
    assert_eq!(
        t.read_builder_stake(&BuilderStake::find_address(&builder_key, 1).0)
            .await
            .required_2z_amount,
        common::TIER_1GBPS * 10
    );
}
