use std::io;

use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_tools::{Discriminator, DISCRIMINATOR_LEN};
use solana_hash::Hash;
use solana_pubkey::Pubkey;

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, Copy, Default, PartialEq, Eq)]
pub struct AdminKey(Pubkey);

impl AdminKey {
    pub fn new(pubkey: Pubkey) -> Self {
        Self(pubkey)
    }
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq)]
pub enum ConfigureProgramSetting {
    Flag(ConfigureFlag),
    Accountant(Pubkey),
    Sol2zSwapProgram(Pubkey),
    SolanaValidatorFee(u16),
    CalculationGracePeriodSeconds(u32),
    CommunityBurnRateParameters {
        limit: u32,
        dz_epochs_to_increasing: u32,
        dz_epochs_to_limit: u32,
        initial_rate: Option<u32>,
    },
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq)]
pub enum ConfigureFlag {
    IsPaused(bool),
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq)]
pub enum ConfigureDistributionData {
    SolanaValidatorPayments {
        total_owed: u64,
        merkle_root: Hash,
    },
    ContributorRewards {
        total_contributors: u32,
        merkle_root: Hash,
    },
}

#[derive(Debug, Clone, PartialEq)]
pub enum RevenueDistributionInstructionData {
    InitializeProgram,
    SetAdmin(Pubkey),
    ConfigureProgram(ConfigureProgramSetting),
    InitializeJournal,
    InitializeDistribution,
    ConfigureDistribution(ConfigureDistributionData),
}

impl From<AdminKey> for RevenueDistributionInstructionData {
    fn from(key: AdminKey) -> Self {
        Self::SetAdmin(key.0)
    }
}

impl TryFrom<AdminKey> for Vec<u8> {
    type Error = io::Error;

    fn try_from(key: AdminKey) -> Result<Self, Self::Error> {
        let ix_data = RevenueDistributionInstructionData::from(key);
        borsh::to_vec(&ix_data)
    }
}

impl From<ConfigureProgramSetting> for RevenueDistributionInstructionData {
    fn from(setting: ConfigureProgramSetting) -> Self {
        Self::ConfigureProgram(setting)
    }
}

impl TryFrom<ConfigureProgramSetting> for Vec<u8> {
    type Error = io::Error;

    fn try_from(setting: ConfigureProgramSetting) -> Result<Self, Self::Error> {
        let ix_data = RevenueDistributionInstructionData::from(setting);
        borsh::to_vec(&ix_data)
    }
}

impl From<ConfigureDistributionData> for RevenueDistributionInstructionData {
    fn from(data: ConfigureDistributionData) -> Self {
        Self::ConfigureDistribution(data)
    }
}

impl TryFrom<ConfigureDistributionData> for Vec<u8> {
    type Error = io::Error;

    fn try_from(data: ConfigureDistributionData) -> Result<Self, Self::Error> {
        let ix_data = RevenueDistributionInstructionData::from(data);
        borsh::to_vec(&ix_data)
    }
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
    pub const INITIALIZE_DISTRIBUTION: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::initialize_distribution");
    pub const CONFIGURE_DISTRIBUTION: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::configure_distribution");
}

impl BorshDeserialize for RevenueDistributionInstructionData {
    fn deserialize_reader<R: io::Read>(reader: &mut R) -> std::io::Result<Self> {
        match Discriminator::deserialize_reader(reader)? {
            Self::INITIALIZE_PROGRAM => Ok(Self::InitializeProgram),
            Self::SET_ADMIN => AdminKey::deserialize_reader(reader).map(Into::into),
            Self::CONFIGURE_PROGRAM => {
                ConfigureProgramSetting::deserialize_reader(reader).map(Into::into)
            }
            Self::INITIALIZE_JOURNAL => Ok(Self::InitializeJournal),
            Self::INITIALIZE_DISTRIBUTION => Ok(Self::InitializeDistribution),
            Self::CONFIGURE_DISTRIBUTION => {
                ConfigureDistributionData::deserialize_reader(reader).map(Into::into)
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
            Self::SetAdmin(key) => {
                Self::SET_ADMIN.serialize(writer)?;
                key.serialize(writer)
            }
            Self::ConfigureProgram(setting) => {
                Self::CONFIGURE_PROGRAM.serialize(writer)?;
                setting.serialize(writer)
            }
            Self::InitializeJournal => Self::INITIALIZE_JOURNAL.serialize(writer),
            Self::InitializeDistribution => Self::INITIALIZE_DISTRIBUTION.serialize(writer),
            Self::ConfigureDistribution(data) => {
                Self::CONFIGURE_DISTRIBUTION.serialize(writer)?;
                data.serialize(writer)
            } // Self::Create2zFeeAccount => Self::CREATE_2Z_FEE_ACCOUNT.serialize(writer),
              // Self::SetSolSwapProgram => Self::SET_SOL_SWAP_PROGRAM.serialize(writer),
              // Self::InitializeDistribution => Self::INITIALIZE_DISTRIBUTION.serialize(writer),
              // Self::SetContributorProportion => Self::UPDATE_CONTRIBUTOR_PROPORTION.serialize(writer),
              // Self::SweepToDistribution => Self::SWEEP_TO_DISTRIBUTION.serialize(writer),
              // Self::DistributeRevenue => Self::DISTRIBUTE_REVENUE.serialize(writer),
        }
    }
}
