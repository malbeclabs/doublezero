pub mod account;

//

use std::io;

use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_tools::{Discriminator, DISCRIMINATOR_LEN};
use solana_pubkey::Pubkey;
use svm_hash::{merkle::MerkleProof, sha2::Hash};

use crate::types::{EpochDuration, RewardShare, SolanaValidatorDebt};

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub enum ProgramConfiguration {
    Flag(ProgramFlagConfiguration),
    DebtAccountant(Pubkey),
    RewardsAccountant(Pubkey),
    ContributorManager(Pubkey),
    PlaceholderKey(Pubkey),
    Sol2zSwapProgram(Pubkey),
    SolanaValidatorFeeParameters {
        base_block_rewards_pct: u16,
        priority_block_rewards_pct: u16,
        inflation_rewards_pct: u16,
        jito_tips_pct: u16,
        fixed_sol_amount: u32,
        _unused: [u8; 28],
    },
    CalculationGracePeriodMinutes(u16),
    CommunityBurnRateParameters {
        limit: u32,
        dz_epochs_to_increasing: EpochDuration,
        dz_epochs_to_limit: EpochDuration,
        initial_rate: Option<u32>,
    },
    PlaceholderRelayLamports(u32),
    DistributeRewardsRelayLamports(u32),
    MinimumEpochDurationToFinalizeRewards(u8),
    DistributionInitializationGracePeriodMinutes(u16),
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub enum ProgramFlagConfiguration {
    IsPaused(bool),
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub enum ContributorRewardsConfiguration {
    Recipients(Vec<(Pubkey, u16)>),
    IsSetRewardsManagerBlocked(bool),
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub enum DistributionMerkleRootKind {
    SolanaValidatorDebt(SolanaValidatorDebt),
    RewardShare(RewardShare),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RevenueDistributionInstructionData {
    InitializeProgram,
    MigrateProgramAccounts,
    SetAdmin(Pubkey),
    ConfigureProgram(ProgramConfiguration),
    InitializeJournal,
    InitializeDistribution,
    ConfigureDistributionDebt {
        total_validators: u32,
        total_debt: u64,
        merkle_root: Hash,
    },
    FinalizeDistributionDebt,
    ConfigureDistributionRewards {
        total_contributors: u32,
        merkle_root: Hash,
    },
    FinalizeDistributionRewards,
    DistributeRewards {
        unit_share: u32,
        economic_burn_rate: u32,
        proof: MerkleProof,
    },
    InitializeContributorRewards(Pubkey),
    SetRewardsManager(Pubkey),
    ConfigureContributorRewards(ContributorRewardsConfiguration),
    VerifyDistributionMerkleRoot {
        kind: DistributionMerkleRootKind,
        proof: MerkleProof,
    },
    InitializeSolanaValidatorDeposit(Pubkey),
    PaySolanaValidatorDebt {
        amount: u64,
        proof: MerkleProof,
    },
    ForgiveSolanaValidatorDebt {
        debt: SolanaValidatorDebt,
        proof: MerkleProof,
    },
    InitializeSwapDestination,
    SweepDistributionTokens,
    WithdrawSol(u64),
}

impl RevenueDistributionInstructionData {
    pub const INITIALIZE_PROGRAM: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::initialize_program");
    pub const MIGRATE_PROGRAM_ACCOUNTS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::migrate_program_accounts");
    pub const SET_ADMIN: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::set_admin");
    pub const CONFIGURE_PROGRAM: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::configure_program");
    pub const INITIALIZE_JOURNAL: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::initialize_journal");
    pub const INITIALIZE_DISTRIBUTION: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::initialize_distribution");
    pub const CONFIGURE_DISTRIBUTION_DEBT: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::configure_distribution_debt");
    pub const FINALIZE_DISTRIBUTION_DEBT: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::finalize_distribution_debt");
    pub const CONFIGURE_DISTRIBUTION_REWARDS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::configure_distribution_rewards");
    pub const FINALIZE_DISTRIBUTION_REWARDS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::finalize_distribution_rewards");
    pub const DISTRIBUTE_REWARDS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::distribute_rewards");
    pub const INITIALIZE_CONTRIBUTOR_REWARDS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::initialize_contributor_rewards");
    pub const SET_REWARDS_MANAGER: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::set_rewards_manager");
    pub const CONFIGURE_CONTRIBUTOR_REWARDS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::configure_contributor_rewards");
    pub const VERIFY_DISTRIBUTION_MERKLE_ROOT: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::verify_distribution_merkle_root");
    pub const INITIALIZE_SOLANA_VALIDATOR_DEPOSIT: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::initialize_solana_validator_deposit");
    pub const PAY_SOLANA_VALIDATOR_DEBT: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::pay_solana_validator_debt");
    pub const FORGIVE_SOLANA_VALIDATOR_DEBT: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::forgive_solana_validator_debt");
    pub const INITIALIZE_SWAP_DESTINATION: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::initialize_swap_destination");
    pub const WITHDRAW_SOL: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::withdraw_sol");

    //
    // Versioned instruction selectors.
    //

    pub const SWEEP_DISTRIBUTION_TOKENS_V1: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::sweep_distribution_tokens::v1");
}

impl BorshDeserialize for RevenueDistributionInstructionData {
    fn deserialize_reader<R: io::Read>(reader: &mut R) -> std::io::Result<Self> {
        match Discriminator::deserialize_reader(reader)? {
            Self::INITIALIZE_PROGRAM => Ok(Self::InitializeProgram),
            Self::MIGRATE_PROGRAM_ACCOUNTS => Ok(Self::MigrateProgramAccounts),
            Self::SET_ADMIN => BorshDeserialize::deserialize_reader(reader).map(Self::SetAdmin),
            Self::CONFIGURE_PROGRAM => {
                BorshDeserialize::deserialize_reader(reader).map(Self::ConfigureProgram)
            }
            Self::INITIALIZE_JOURNAL => Ok(Self::InitializeJournal),
            Self::INITIALIZE_DISTRIBUTION => Ok(Self::InitializeDistribution),
            Self::CONFIGURE_DISTRIBUTION_DEBT => {
                let total_validators = BorshDeserialize::deserialize_reader(reader)?;
                let total_debt = BorshDeserialize::deserialize_reader(reader)?;
                let merkle_root = BorshDeserialize::deserialize_reader(reader)?;

                Ok(Self::ConfigureDistributionDebt {
                    total_validators,
                    total_debt,
                    merkle_root,
                })
            }
            Self::FINALIZE_DISTRIBUTION_DEBT => Ok(Self::FinalizeDistributionDebt),
            Self::CONFIGURE_DISTRIBUTION_REWARDS => {
                let total_contributors = BorshDeserialize::deserialize_reader(reader)?;
                let merkle_root = BorshDeserialize::deserialize_reader(reader)?;

                Ok(Self::ConfigureDistributionRewards {
                    total_contributors,
                    merkle_root,
                })
            }
            Self::FINALIZE_DISTRIBUTION_REWARDS => Ok(Self::FinalizeDistributionRewards),
            Self::DISTRIBUTE_REWARDS => {
                let unit_share = BorshDeserialize::deserialize_reader(reader)?;
                let economic_burn_rate = BorshDeserialize::deserialize_reader(reader)?;
                let proof = BorshDeserialize::deserialize_reader(reader)?;

                Ok(Self::DistributeRewards {
                    unit_share,
                    economic_burn_rate,
                    proof,
                })
            }
            Self::INITIALIZE_CONTRIBUTOR_REWARDS => {
                BorshDeserialize::deserialize_reader(reader).map(Self::InitializeContributorRewards)
            }
            Self::SET_REWARDS_MANAGER => {
                BorshDeserialize::deserialize_reader(reader).map(Self::SetRewardsManager)
            }
            Self::CONFIGURE_CONTRIBUTOR_REWARDS => {
                ContributorRewardsConfiguration::deserialize_reader(reader)
                    .map(Self::ConfigureContributorRewards)
            }
            Self::VERIFY_DISTRIBUTION_MERKLE_ROOT => {
                let kind = BorshDeserialize::deserialize_reader(reader)?;
                let proof = BorshDeserialize::deserialize_reader(reader)?;

                Ok(Self::VerifyDistributionMerkleRoot { kind, proof })
            }
            Self::INITIALIZE_SOLANA_VALIDATOR_DEPOSIT => {
                BorshDeserialize::deserialize_reader(reader)
                    .map(Self::InitializeSolanaValidatorDeposit)
            }
            Self::PAY_SOLANA_VALIDATOR_DEBT => {
                let amount = BorshDeserialize::deserialize_reader(reader)?;
                let proof = BorshDeserialize::deserialize_reader(reader)?;

                Ok(Self::PaySolanaValidatorDebt { amount, proof })
            }
            Self::FORGIVE_SOLANA_VALIDATOR_DEBT => {
                let debt = BorshDeserialize::deserialize_reader(reader)?;
                let proof = BorshDeserialize::deserialize_reader(reader)?;

                Ok(Self::ForgiveSolanaValidatorDebt { debt, proof })
            }
            Self::INITIALIZE_SWAP_DESTINATION => Ok(Self::InitializeSwapDestination),
            Self::SWEEP_DISTRIBUTION_TOKENS_V1 => Ok(Self::SweepDistributionTokens),
            Self::WITHDRAW_SOL => {
                BorshDeserialize::deserialize_reader(reader).map(Self::WithdrawSol)
            }
            _ => Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "Invalid discriminator",
            )),
        }
    }
}

impl BorshSerialize for RevenueDistributionInstructionData {
    fn serialize<W: io::Write>(&self, writer: &mut W) -> io::Result<()> {
        match self {
            Self::InitializeProgram => Self::INITIALIZE_PROGRAM.serialize(writer),
            Self::MigrateProgramAccounts => Self::MIGRATE_PROGRAM_ACCOUNTS.serialize(writer),
            Self::SetAdmin(admin_key) => {
                Self::SET_ADMIN.serialize(writer)?;
                admin_key.serialize(writer)
            }
            Self::ConfigureProgram(setting) => {
                Self::CONFIGURE_PROGRAM.serialize(writer)?;
                setting.serialize(writer)
            }
            Self::InitializeJournal => Self::INITIALIZE_JOURNAL.serialize(writer),
            Self::InitializeDistribution => Self::INITIALIZE_DISTRIBUTION.serialize(writer),
            Self::ConfigureDistributionDebt {
                total_validators,
                total_debt,
                merkle_root,
            } => {
                Self::CONFIGURE_DISTRIBUTION_DEBT.serialize(writer)?;
                total_validators.serialize(writer)?;
                total_debt.serialize(writer)?;
                merkle_root.serialize(writer)
            }
            Self::FinalizeDistributionDebt => Self::FINALIZE_DISTRIBUTION_DEBT.serialize(writer),
            Self::ConfigureDistributionRewards {
                total_contributors,
                merkle_root,
            } => {
                Self::CONFIGURE_DISTRIBUTION_REWARDS.serialize(writer)?;
                total_contributors.serialize(writer)?;
                merkle_root.serialize(writer)
            }
            Self::FinalizeDistributionRewards => {
                Self::FINALIZE_DISTRIBUTION_REWARDS.serialize(writer)
            }
            Self::DistributeRewards {
                unit_share,
                economic_burn_rate,
                proof,
            } => {
                Self::DISTRIBUTE_REWARDS.serialize(writer)?;
                unit_share.serialize(writer)?;
                economic_burn_rate.serialize(writer)?;
                proof.serialize(writer)
            }
            Self::InitializeContributorRewards(service_key) => {
                Self::INITIALIZE_CONTRIBUTOR_REWARDS.serialize(writer)?;
                service_key.serialize(writer)
            }
            Self::SetRewardsManager(rewards_manager_key) => {
                Self::SET_REWARDS_MANAGER.serialize(writer)?;
                rewards_manager_key.serialize(writer)
            }
            Self::ConfigureContributorRewards(setting) => {
                Self::CONFIGURE_CONTRIBUTOR_REWARDS.serialize(writer)?;
                setting.serialize(writer)
            }
            Self::VerifyDistributionMerkleRoot { kind, proof } => {
                Self::VERIFY_DISTRIBUTION_MERKLE_ROOT.serialize(writer)?;
                kind.serialize(writer)?;
                proof.serialize(writer)
            }
            Self::InitializeSolanaValidatorDeposit(solana_validator_deposit_key) => {
                Self::INITIALIZE_SOLANA_VALIDATOR_DEPOSIT.serialize(writer)?;
                solana_validator_deposit_key.serialize(writer)
            }
            Self::PaySolanaValidatorDebt { amount, proof } => {
                Self::PAY_SOLANA_VALIDATOR_DEBT.serialize(writer)?;
                amount.serialize(writer)?;
                proof.serialize(writer)
            }
            Self::ForgiveSolanaValidatorDebt { debt, proof } => {
                Self::FORGIVE_SOLANA_VALIDATOR_DEBT.serialize(writer)?;
                debt.serialize(writer)?;
                proof.serialize(writer)
            }
            Self::InitializeSwapDestination => Self::INITIALIZE_SWAP_DESTINATION.serialize(writer),
            Self::SweepDistributionTokens => Self::SWEEP_DISTRIBUTION_TOKENS_V1.serialize(writer),
            Self::WithdrawSol(amount) => {
                Self::WITHDRAW_SOL.serialize(writer)?;
                amount.serialize(writer)
            }
        }
    }
}
