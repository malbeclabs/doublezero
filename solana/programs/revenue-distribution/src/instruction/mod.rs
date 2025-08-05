pub mod account;

//

use std::io;

use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_tools::{Discriminator, DISCRIMINATOR_LEN};
use solana_hash::Hash;
use solana_pubkey::Pubkey;

use crate::types::{DoubleZeroEpoch, EpochDuration};

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
pub enum DistributionConfiguration {
    UpdateSolanaValidatorPayments {
        total_lamports_owed: u64,
        merkle_root: Hash,
    },
    FinalizeSolanaValidatorPayments,
    UpdateContributorRewards {
        total_contributors: u32,
        merkle_root: Hash,
    },
    FinalizeContributorRewards,
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub enum ContributorRewardsConfiguration {
    Recipients(Vec<(Pubkey, u16)>),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RevenueDistributionInstructionData {
    InitializeProgram,
    SetAdmin(Pubkey),
    ConfigureProgram(ProgramConfiguration),
    InitializeJournal,
    ConfigureJournal(JournalConfiguration),
    InitializeDistribution,
    ConfigureDistribution(DistributionConfiguration),
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
}

impl RevenueDistributionInstructionData {
    pub const INITIALIZE_PROGRAM: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::initialize_program");
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
    pub const CONFIGURE_DISTRIBUTION: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::configure_distribution");
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
}

impl BorshDeserialize for RevenueDistributionInstructionData {
    fn deserialize_reader<R: io::Read>(reader: &mut R) -> std::io::Result<Self> {
        match Discriminator::deserialize_reader(reader)? {
            Self::INITIALIZE_PROGRAM => Ok(Self::InitializeProgram),
            Self::SET_ADMIN => BorshDeserialize::deserialize_reader(reader).map(Self::SetAdmin),
            Self::CONFIGURE_PROGRAM => {
                BorshDeserialize::deserialize_reader(reader).map(Self::ConfigureProgram)
            }
            Self::CONFIGURE_JOURNAL => {
                BorshDeserialize::deserialize_reader(reader).map(Self::ConfigureJournal)
            }
            Self::INITIALIZE_JOURNAL => Ok(Self::InitializeJournal),
            Self::INITIALIZE_DISTRIBUTION => Ok(Self::InitializeDistribution),
            Self::CONFIGURE_DISTRIBUTION => DistributionConfiguration::deserialize_reader(reader)
                .map(Self::ConfigureDistribution),
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
            Self::ConfigureDistribution(setting) => {
                Self::CONFIGURE_DISTRIBUTION.serialize(writer)?;
                setting.serialize(writer)
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
        }
    }
}
