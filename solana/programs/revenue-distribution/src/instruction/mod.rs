pub mod account;

//

use std::io;

use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_tools::{Discriminator, DISCRIMINATOR_LEN};
use solana_pubkey::Pubkey;
use svm_hash::{merkle::MerkleProof, sha2::Hash};

use crate::types::{DoubleZeroEpoch, EpochDuration, SolanaValidatorPayment};

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub enum ProgramConfiguration {
    Flag(ProgramFlagConfiguration),
    PaymentsAccountant(Pubkey),
    RewardsAccountant(Pubkey),
    ContributorManager(Pubkey),
    DoubleZeroLedgerSentinel(Pubkey),
    Sol2zSwapProgram(Pubkey),
    SolanaValidatorFeeParameters {
        base_block_rewards: u16,
        priority_block_rewards: u16,
        inflation_rewards: u16,
        jito_tips: u16,
        _unused: [u8; 32],
    },
    CalculationGracePeriodSeconds(u32),
    CommunityBurnRateParameters {
        limit: u32,
        dz_epochs_to_increasing: EpochDuration,
        dz_epochs_to_limit: EpochDuration,
        initial_rate: Option<u32>,
    },
    PrepaidConnectionTerminationRelayLamports(u32),
    ContributorRewardClaimLamports(u32),
    MinimumEpochDurationToFinalizeRewards(u16),
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub enum JournalConfiguration {
    ActivationCost(u32),
    CostPerDoubleZeroEpoch(u32),
    EntryBoundaries {
        minimum_prepaid_dz_epochs: u16,
        maximum_entries: u16,
    },
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub enum ProgramFlagConfiguration {
    IsPaused(bool),
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub enum DistributionPaymentsConfiguration {
    UpdateSolanaValidatorPayments {
        total_validators: u32,
        total_debt: u64,
        merkle_root: Hash,
    },
    UpdateUncollectibleSol(u64),
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub enum ContributorRewardsConfiguration {
    Recipients(Vec<(Pubkey, u16)>),
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub enum DistributionMerkleRootKind {
    SolanaValidatorPayment(SolanaValidatorPayment),
    RewardShare(
        // TODO: Add rewards share struct when created
    ),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RevenueDistributionInstructionData {
    InitializeProgram,
    MigrateProgramAccounts,
    SetAdmin(Pubkey),
    ConfigureProgram(ProgramConfiguration),
    InitializeJournal,
    ConfigureJournal(JournalConfiguration),
    InitializeDistribution,
    ConfigureDistributionPayments(DistributionPaymentsConfiguration),
    FinalizeDistributionPayments,
    ConfigureDistributionRewards {
        total_contributors: u32,
        merkle_root: Hash,
    },
    FinalizeDistributionRewards,
    InitializePrepaidConnection {
        user_key: Pubkey,
        decimals: u8,
    },
    GrantPrepaidConnectionAccess,
    DenyPrepaidConnectionAccess,
    LoadPrepaidConnection {
        valid_through_dz_epoch: DoubleZeroEpoch,
        decimals: u8,
    },
    TerminatePrepaidConnection,
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
    pub const CONFIGURE_JOURNAL: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::configure_journal");
    pub const INITIALIZE_DISTRIBUTION: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::initialize_distribution");
    pub const CONFIGURE_DISTRIBUTION_PAYMENTS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::configure_distribution_payments");
    pub const FINALIZE_DISTRIBUTION_PAYMENTS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::finalize_distribution_payments");
    pub const CONFIGURE_DISTRIBUTION_REWARDS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::configure_distribution_rewards");
    pub const FINALIZE_DISTRIBUTION_REWARDS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::finalize_distribution_rewards");
    pub const INITIALIZE_PREPAID_CONNECTION: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::initialize_prepaid_connection");
    pub const GRANT_PREPAID_CONNECTION_ACCESS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::grant_prepaid_connection_access");
    pub const DENY_PREPAID_CONNECTION_ACCESS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::deny_prepaid_connection_access");
    pub const LOAD_PREPAID_CONNECTION: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::load_prepaid_connection");
    pub const TERMINATE_PREPAID_CONNECTION: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::terminate_prepaid_connection");
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
            Self::CONFIGURE_JOURNAL => {
                BorshDeserialize::deserialize_reader(reader).map(Self::ConfigureJournal)
            }
            Self::INITIALIZE_JOURNAL => Ok(Self::InitializeJournal),
            Self::INITIALIZE_DISTRIBUTION => Ok(Self::InitializeDistribution),
            Self::CONFIGURE_DISTRIBUTION_PAYMENTS => {
                DistributionPaymentsConfiguration::deserialize_reader(reader)
                    .map(Self::ConfigureDistributionPayments)
            }
            Self::FINALIZE_DISTRIBUTION_PAYMENTS => Ok(Self::FinalizeDistributionPayments),
            Self::CONFIGURE_DISTRIBUTION_REWARDS => {
                let total_contributors = BorshDeserialize::deserialize_reader(reader)?;
                let merkle_root = BorshDeserialize::deserialize_reader(reader)?;

                Ok(Self::ConfigureDistributionRewards {
                    total_contributors,
                    merkle_root,
                })
            }
            Self::FINALIZE_DISTRIBUTION_REWARDS => Ok(Self::FinalizeDistributionRewards),
            Self::INITIALIZE_PREPAID_CONNECTION => {
                let user_key = BorshDeserialize::deserialize_reader(reader)?;
                let decimals = BorshDeserialize::deserialize_reader(reader)?;

                Ok(Self::InitializePrepaidConnection { user_key, decimals })
            }
            Self::GRANT_PREPAID_CONNECTION_ACCESS => Ok(Self::GrantPrepaidConnectionAccess),
            Self::DENY_PREPAID_CONNECTION_ACCESS => Ok(Self::DenyPrepaidConnectionAccess),
            Self::LOAD_PREPAID_CONNECTION => {
                let valid_through_dz_epoch = BorshDeserialize::deserialize_reader(reader)?;
                let decimals = BorshDeserialize::deserialize_reader(reader)?;

                Ok(Self::LoadPrepaidConnection {
                    valid_through_dz_epoch,
                    decimals,
                })
            }
            Self::TERMINATE_PREPAID_CONNECTION => Ok(Self::TerminatePrepaidConnection),
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
            Self::ConfigureJournal(setting) => {
                Self::CONFIGURE_JOURNAL.serialize(writer)?;
                setting.serialize(writer)
            }
            Self::InitializeJournal => Self::INITIALIZE_JOURNAL.serialize(writer),
            Self::InitializeDistribution => Self::INITIALIZE_DISTRIBUTION.serialize(writer),
            Self::ConfigureDistributionPayments(setting) => {
                Self::CONFIGURE_DISTRIBUTION_PAYMENTS.serialize(writer)?;
                setting.serialize(writer)
            }
            Self::FinalizeDistributionPayments => {
                Self::FINALIZE_DISTRIBUTION_PAYMENTS.serialize(writer)
            }
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
            Self::InitializePrepaidConnection { user_key, decimals } => {
                Self::INITIALIZE_PREPAID_CONNECTION.serialize(writer)?;
                user_key.serialize(writer)?;
                decimals.serialize(writer)
            }
            Self::GrantPrepaidConnectionAccess => {
                Self::GRANT_PREPAID_CONNECTION_ACCESS.serialize(writer)
            }
            Self::DenyPrepaidConnectionAccess => {
                Self::DENY_PREPAID_CONNECTION_ACCESS.serialize(writer)
            }
            Self::LoadPrepaidConnection {
                valid_through_dz_epoch,
                decimals,
            } => {
                Self::LOAD_PREPAID_CONNECTION.serialize(writer)?;
                valid_through_dz_epoch.serialize(writer)?;
                decimals.serialize(writer)
            }
            Self::TerminatePrepaidConnection => {
                Self::TERMINATE_PREPAID_CONNECTION.serialize(writer)
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
        }
    }
}
