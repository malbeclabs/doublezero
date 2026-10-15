mod common;

//

use doublezero_program_tools::instruction::try_build_instruction;
use doublezero_revenue_distribution::{
    instruction::{
        account::DistributeMonthlyContributorRewardsAccounts, ContributorRewardsConfiguration,
        ProgramConfiguration, ProgramFlagConfiguration, RevenueDistributionInstructionData,
    },
    integration::find_integration_bucket_address,
    types::RewardShare,
    DOUBLEZERO_MINT_KEY, ID,
};
use mock_rewards_integration::state::MockMonthlyRewardsBucket;
use solana_program_test::{tokio, BanksClientError};
use solana_pubkey::{pubkey, Pubkey};
use solana_sdk::{
    instruction::InstructionError,
    signature::{Keypair, Signer},
    transaction::TransactionError,
};
use spl_associated_token_account_interface::address::get_associated_token_address;
use svm_hash::merkle::{merkle_root_from_indexed_pod_leaves, MerkleProof};

//
// Setup.
//

const YEAR: u16 = 2026;
const MONTH: u8 = 9;
/// 2026-10-01T00:00:00Z.
const MONTH_END_TIMESTAMP: i64 = 1_790_812_800;
const COLLECTED_2Z_AMOUNT: u64 = 123_456_789_012;

struct Contributor {
    reward_share: RewardShare,
    proof: MerkleProof,
    recipients: Vec<(Pubkey, u16)>,
}

impl Contributor {
    fn recipient_keys(&self) -> Vec<&Pubkey> {
        self.recipients
            .iter()
            .map(|(recipient_key, _)| recipient_key)
            .collect()
    }

    fn distribute_accounts(&self) -> DistributeMonthlyContributorRewardsAccounts {
        DistributeMonthlyContributorRewardsAccounts::new(
            YEAR,
            MONTH,
            &self.reward_share.contributor_key,
            &DOUBLEZERO_MINT_KEY,
            &self.recipient_keys(),
        )
    }
}

struct DistributeMonthlyRewardsSetup {
    test_setup: common::ProgramTestWithOwner,
    admin_signer: Keypair,
    rewards_accountant_signer: Keypair,
    contributors: Vec<Contributor>,
}

/// Uneven unit shares and recipient splits, so both floors leave dust.
fn uneven_contributor_configs() -> Vec<(u32, Vec<(Pubkey, u16)>)> {
    vec![
        (
            500_000_001,
            vec![(Pubkey::new_unique(), 3_333), (Pubkey::new_unique(), 6_667)],
        ),
        (
            333_333_333,
            vec![
                (Pubkey::new_unique(), 5_000),
                (Pubkey::new_unique(), 2_500),
                (Pubkey::new_unique(), 2_500),
            ],
        ),
        (166_666_666, vec![(Pubkey::new_unique(), 10_000)]),
    ]
}

/// Funds the month's bucket and posts one leaf per config, leaving collect and
/// finalize to the caller. Empty recipients leave a contributor unconfigured.
async fn setup_for_distribute_monthly_rewards(
    contributor_configs: Vec<(u32, Vec<(Pubkey, u16)>)>,
) -> DistributeMonthlyRewardsSetup {
    let mut test_setup = common::start_test().await;
    let configured = test_setup.setup_configured_program().await.unwrap();

    let contributor_manager_signer = Keypair::new();
    let rewards_manager_signer = Keypair::new();
    let (_, bucket_2z_key) = bucket_keys();

    test_setup
        .configure_program(
            &configured.admin_signer,
            [
                ProgramConfiguration::ContributorManager(contributor_manager_signer.pubkey()),
                ProgramConfiguration::MonthlyContributorRewardsProgram(
                    mock_rewards_integration::ID,
                ),
            ],
        )
        .await
        .unwrap()
        .initialize_monthly_contributor_distribution(YEAR, MONTH)
        .await
        .unwrap()
        .mock_initialize_monthly_rewards_bucket(YEAR, MONTH)
        .await
        .unwrap()
        .transfer_2z(&bucket_2z_key, COLLECTED_2Z_AMOUNT)
        .await
        .unwrap();

    let reward_shares = contributor_configs
        .iter()
        .map(|(unit_share, _)| {
            RewardShare::new(Pubkey::new_unique(), *unit_share, false, 0).unwrap()
        })
        .collect::<Vec<_>>();

    for (reward_share, (_, recipients)) in reward_shares.iter().zip(&contributor_configs) {
        let service_key = &reward_share.contributor_key;

        test_setup
            .initialize_contributor_rewards(service_key)
            .await
            .unwrap();

        if recipients.is_empty() {
            continue;
        }

        for (recipient_key, _) in recipients {
            test_setup.create_2z_ata(recipient_key).await.unwrap();
        }

        test_setup
            .set_rewards_manager(
                service_key,
                &contributor_manager_signer,
                &rewards_manager_signer.pubkey(),
            )
            .await
            .unwrap()
            .configure_contributor_rewards(
                service_key,
                &rewards_manager_signer,
                [ContributorRewardsConfiguration::Recipients(
                    recipients.clone(),
                )],
            )
            .await
            .unwrap();
    }

    let merkle_root =
        merkle_root_from_indexed_pod_leaves(&reward_shares, Some(RewardShare::LEAF_PREFIX))
            .unwrap();

    test_setup
        .configure_monthly_contributor_distribution_rewards(
            YEAR,
            MONTH,
            &configured.rewards_accountant_signer,
            reward_shares.len() as u32,
            merkle_root,
        )
        .await
        .unwrap()
        .set_unix_timestamp(MONTH_END_TIMESTAMP)
        .await
        .unwrap();

    let contributors = contributor_configs
        .into_iter()
        .zip(&reward_shares)
        .enumerate()
        .map(|(i, ((_, recipients), reward_share))| Contributor {
            reward_share: *reward_share,
            proof: MerkleProof::from_indexed_pod_leaves(
                &reward_shares,
                i.try_into().unwrap(),
                Some(RewardShare::LEAF_PREFIX),
            )
            .unwrap(),
            recipients,
        })
        .collect();

    DistributeMonthlyRewardsSetup {
        test_setup,
        admin_signer: configured.admin_signer,
        rewards_accountant_signer: configured.rewards_accountant_signer,
        contributors,
    }
}

fn bucket_keys() -> (Pubkey, Pubkey) {
    let bucket_key = MockMonthlyRewardsBucket::find_address(YEAR, MONTH).0;
    let bucket_2z_key =
        find_integration_bucket_address(&mock_rewards_integration::ID, &bucket_key).0;

    (bucket_key, bucket_2z_key)
}

async fn collect(test_setup: &mut common::ProgramTestWithOwner) {
    let (bucket_key, bucket_2z_key) = bucket_keys();

    test_setup
        .collect_monthly_contributor_rewards(
            YEAR,
            MONTH,
            &mock_rewards_integration::ID,
            &bucket_key,
            &bucket_2z_key,
        )
        .await
        .unwrap();
}

async fn collect_and_finalize(setup: &mut DistributeMonthlyRewardsSetup) {
    collect(&mut setup.test_setup).await;

    setup
        .test_setup
        .finalize_monthly_contributor_distribution_rewards(
            YEAR,
            MONTH,
            &setup.rewards_accountant_signer,
        )
        .await
        .unwrap();
}

async fn simulate_distribute(
    test_setup: &mut common::ProgramTestWithOwner,
    accounts: DistributeMonthlyContributorRewardsAccounts,
    unit_share: u32,
    proof: MerkleProof,
) -> Result<(TransactionError, Vec<String>), BanksClientError> {
    let distribute_ix = try_build_instruction(
        &ID,
        accounts,
        &RevenueDistributionInstructionData::DistributeMonthlyContributorRewards {
            unit_share,
            proof,
        },
    )
    .unwrap();

    test_setup
        .unwrap_simulation_error(&[distribute_ix], &[])
        .await
}

//
// Distribute monthly contributor rewards — happy path.
//

#[tokio::test]
async fn test_distribute_monthly_contributor_rewards() {
    let mut setup = setup_for_distribute_monthly_rewards(uneven_contributor_configs()).await;
    collect_and_finalize(&mut setup).await;

    let DistributeMonthlyRewardsSetup {
        mut test_setup,
        contributors,
        ..
    } = setup;

    let (_, before, _, _, _) = test_setup
        .fetch_monthly_contributor_distribution(YEAR, MONTH)
        .await;

    // floor(share * floor(unit_share * COLLECTED_2Z_AMOUNT / 1e9) / 10,000).
    let expected_recipient_amounts = vec![
        vec![20_574_073_929, 41_154_320_699],
        vec![20_576_131_481, 10_288_065_740, 10_288_065_740],
        vec![20_576_131_419],
    ];

    // The test payer holds no authority, so this also shows anyone may pay.
    for (i, (contributor, expected_amounts)) in contributors
        .iter()
        .zip(&expected_recipient_amounts)
        .enumerate()
    {
        let recipient_keys = contributor.recipient_keys();

        test_setup
            .distribute_monthly_contributor_rewards(
                YEAR,
                MONTH,
                &contributor.reward_share,
                &recipient_keys,
                contributor.proof.clone(),
            )
            .await
            .unwrap();

        for (recipient_key, expected_amount) in recipient_keys.into_iter().zip(expected_amounts) {
            let recipient_ata = test_setup
                .fetch_token_account(&get_associated_token_address(
                    recipient_key,
                    &DOUBLEZERO_MINT_KEY,
                ))
                .await
                .unwrap();
            assert_eq!(recipient_ata.amount, *expected_amount);
        }

        let (tx_err, program_logs) = simulate_distribute(
            &mut test_setup,
            contributor.distribute_accounts(),
            contributor.reward_share.unit_share,
            contributor.proof.clone(),
        )
        .await
        .unwrap();
        assert_eq!(
            tx_err,
            TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
        );

        let expected_log = if i + 1 == contributors.len() {
            "Program log: All rewards have already been distributed".to_string()
        } else {
            format!("Program log: Merkle leaf index {i} has already been processed")
        };
        assert_eq!(program_logs.get(3).unwrap(), &expected_log);
    }

    let (_, monthly_distribution, remaining_data, _, monthly_2z_token_pda) = test_setup
        .fetch_monthly_contributor_distribution(YEAR, MONTH)
        .await;

    let mut expected = before;
    expected.distributed_rewards_count = 3;
    expected.distributed_2z_amount = expected_recipient_amounts.iter().flatten().sum();
    assert_eq!(monthly_distribution, expected);
    assert_eq!(remaining_data, [0b111]);

    // Collected minus distributed: 2 base units of dust from contributor
    // amounts and 2 from recipient splits.
    assert_eq!(monthly_2z_token_pda.amount, 4);
}

#[tokio::test]
async fn test_distribute_monthly_contributor_rewards_to_squads_vault() {
    // Seeds as in doublezero-solana-client-tools' `squads::find_vault_address`.
    let (vault_key, _) = Pubkey::find_program_address(
        &[b"multisig", Pubkey::new_unique().as_ref(), b"vault", &[0]],
        &pubkey!("SQDS4ep65T869zMMBKyuUq6aD6EgTu8psMjkvj52pCf"),
    );
    assert!(!vault_key.is_on_curve());

    let mut setup =
        setup_for_distribute_monthly_rewards(vec![(1_000_000_000, vec![(vault_key, 10_000)])])
            .await;
    collect_and_finalize(&mut setup).await;

    let DistributeMonthlyRewardsSetup {
        mut test_setup,
        contributors,
        ..
    } = setup;
    let contributor = &contributors[0];

    test_setup
        .distribute_monthly_contributor_rewards(
            YEAR,
            MONTH,
            &contributor.reward_share,
            &[&vault_key],
            contributor.proof.clone(),
        )
        .await
        .unwrap();

    let vault_ata = test_setup
        .fetch_token_account(&get_associated_token_address(
            &vault_key,
            &DOUBLEZERO_MINT_KEY,
        ))
        .await
        .unwrap();
    assert_eq!(vault_ata.owner, vault_key);
    assert_eq!(vault_ata.amount, COLLECTED_2Z_AMOUNT);
}

//
// Distribute monthly contributor rewards — failures.
//

#[tokio::test]
async fn test_cannot_distribute_monthly_contributor_rewards_before_collect() {
    let DistributeMonthlyRewardsSetup {
        mut test_setup,
        contributors,
        ..
    } = setup_for_distribute_monthly_rewards(uneven_contributor_configs()).await;
    let contributor = &contributors[0];

    let (tx_err, program_logs) = simulate_distribute(
        &mut test_setup,
        contributor.distribute_accounts(),
        contributor.reward_share.unit_share,
        contributor.proof.clone(),
    )
    .await
    .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Monthly contributor distribution has not been collected yet"
    );
}

#[tokio::test]
async fn test_cannot_distribute_monthly_contributor_rewards_before_finalize() {
    let DistributeMonthlyRewardsSetup {
        mut test_setup,
        contributors,
        ..
    } = setup_for_distribute_monthly_rewards(uneven_contributor_configs()).await;
    let contributor = &contributors[0];

    collect(&mut test_setup).await;

    let (tx_err, program_logs) = simulate_distribute(
        &mut test_setup,
        contributor.distribute_accounts(),
        contributor.reward_share.unit_share,
        contributor.proof.clone(),
    )
    .await
    .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(3).unwrap(),
        "Program log: Monthly contributor distribution rewards are not finalized yet"
    );
}

#[tokio::test]
async fn test_cannot_distribute_monthly_contributor_rewards_when_paused() {
    let mut setup = setup_for_distribute_monthly_rewards(uneven_contributor_configs()).await;
    collect_and_finalize(&mut setup).await;

    let DistributeMonthlyRewardsSetup {
        mut test_setup,
        admin_signer,
        contributors,
        ..
    } = setup;
    let contributor = &contributors[0];

    test_setup
        .configure_program(
            &admin_signer,
            [ProgramConfiguration::Flag(
                ProgramFlagConfiguration::IsPaused(true),
            )],
        )
        .await
        .unwrap();

    let (tx_err, program_logs) = simulate_distribute(
        &mut test_setup,
        contributor.distribute_accounts(),
        contributor.reward_share.unit_share,
        contributor.proof.clone(),
    )
    .await
    .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(2).unwrap(),
        "Program log: Program is paused"
    );
}

#[tokio::test]
async fn test_cannot_distribute_monthly_contributor_rewards_with_wrong_unit_share() {
    let mut setup = setup_for_distribute_monthly_rewards(uneven_contributor_configs()).await;
    collect_and_finalize(&mut setup).await;

    let DistributeMonthlyRewardsSetup {
        mut test_setup,
        contributors,
        ..
    } = setup;
    let contributor = &contributors[0];

    let wrong_unit_share = contributor.reward_share.unit_share + 1;
    let computed_merkle_root = contributor.proof.root_from_pod_leaf(
        &RewardShare::new(
            contributor.reward_share.contributor_key,
            wrong_unit_share,
            false,
            0,
        )
        .unwrap(),
        Some(RewardShare::LEAF_PREFIX),
    );

    for (unit_share, expected_log) in [
        (
            wrong_unit_share,
            format!("Program log: Invalid computed merkle root: {computed_merkle_root}"),
        ),
        (
            u32::MAX,
            format!("Program log: Invalid unit share {}", u32::MAX),
        ),
    ] {
        let (tx_err, program_logs) = simulate_distribute(
            &mut test_setup,
            contributor.distribute_accounts(),
            unit_share,
            contributor.proof.clone(),
        )
        .await
        .unwrap();
        assert_eq!(
            tx_err,
            TransactionError::InstructionError(0, InstructionError::InvalidInstructionData)
        );
        assert_eq!(program_logs.get(4).unwrap(), &expected_log);
    }
}

#[tokio::test]
async fn test_cannot_distribute_monthly_contributor_rewards_without_indexed_proof() {
    let mut setup = setup_for_distribute_monthly_rewards(uneven_contributor_configs()).await;
    collect_and_finalize(&mut setup).await;

    let DistributeMonthlyRewardsSetup {
        mut test_setup,
        contributors,
        ..
    } = setup;
    let contributor = &contributors[0];

    let mut proof = contributor.proof.clone();
    proof.leaf_index = None;

    let (tx_err, program_logs) = simulate_distribute(
        &mut test_setup,
        contributor.distribute_accounts(),
        contributor.reward_share.unit_share,
        proof,
    )
    .await
    .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidInstructionData)
    );
    assert_eq!(
        program_logs.get(2).unwrap(),
        "Program log: Merkle proof must use an indexed tree"
    );
}

#[tokio::test]
async fn test_cannot_distribute_monthly_contributor_rewards_to_wrong_ata() {
    let mut setup = setup_for_distribute_monthly_rewards(uneven_contributor_configs()).await;
    collect_and_finalize(&mut setup).await;

    let DistributeMonthlyRewardsSetup {
        mut test_setup,
        contributors,
        ..
    } = setup;
    let contributor = &contributors[0];

    let mut accounts = contributor.distribute_accounts();
    accounts.recipient_ata_keys[0] =
        get_associated_token_address(&Pubkey::new_unique(), &DOUBLEZERO_MINT_KEY);

    let (tx_err, program_logs) = simulate_distribute(
        &mut test_setup,
        accounts,
        contributor.reward_share.unit_share,
        contributor.proof.clone(),
    )
    .await
    .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(4).unwrap(),
        &format!(
            "Program log: Expected ATA for recipient {} (account 5)",
            contributor.recipients[0].0
        )
    );
}

#[tokio::test]
async fn test_cannot_distribute_monthly_contributor_rewards_without_recipients() {
    let mut contributor_configs = uneven_contributor_configs();
    contributor_configs[0].1.clear();

    let mut setup = setup_for_distribute_monthly_rewards(contributor_configs).await;
    collect_and_finalize(&mut setup).await;

    let DistributeMonthlyRewardsSetup {
        mut test_setup,
        contributors,
        ..
    } = setup;
    let contributor = &contributors[0];

    let (tx_err, program_logs) = simulate_distribute(
        &mut test_setup,
        contributor.distribute_accounts(),
        contributor.reward_share.unit_share,
        contributor.proof.clone(),
    )
    .await
    .unwrap();
    assert_eq!(
        tx_err,
        TransactionError::InstructionError(0, InstructionError::InvalidAccountData)
    );
    assert_eq!(
        program_logs.get(4).unwrap(),
        "Program log: Contributor recipients must be configured"
    );
}
