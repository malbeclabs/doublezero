pub mod account;

//

use std::io;

use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_tools::{DISCRIMINATOR_LEN, Discriminator};
use solana_pubkey::Pubkey;

use crate::oracle::OraclePriceData;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum SolConversionInstructionData {
    /// Set up initial state. Requires the upgrade authority.
    InitializeSystem {
        oracle_key: Pubkey,
        fixed_fill_quantity_lamports: u64,
        price_maximum_age_seconds: i64,
        coefficient: u64,
        max_discount_rate: u64,
        min_discount_rate: u64,
    },

    UpdateConfigurationRegistry {
        oracle_key: Option<Pubkey>,
        fixed_fill_quantity_lamports: Option<u64>,
        price_maximum_age_seconds: Option<i64>,
        coefficient: Option<u64>,
        max_discount_rate: Option<u64>,
        min_discount_rate: Option<u64>,
    },

    SetFillsConsumer(Pubkey),

    AddToDenyList,

    RemoveFromDenyList,

    SetAdmin(Pubkey),

    SetDenyListAuthority,

    /// In other words, pause or unpause the system.
    ToggleSystemState(bool),

    BuySol {
        limit_price: u64,
        oracle_price_data: OraclePriceData,
    },

    GetConversionRate,

    DequeueFills,
}

impl SolConversionInstructionData {
    pub const INITIALIZE_SYSTEM: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"global:initialize_system");
    pub const UPDATE_CONFIGURATION_REGISTRY: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"global:update_configuration_registry");
    pub const SET_FILLS_CONSUMER: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"global:set_fills_consumer");
    pub const ADD_TO_DENY_LIST: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"global:add_to_deny_list");
    pub const REMOVE_FROM_DENY_LIST: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"global:remove_from_deny_list");
    pub const SET_ADMIN: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"global:set_admin");
    pub const SET_DENY_LIST_AUTHORITY: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"global:set_deny_list_authority");
    pub const TOGGLE_SYSTEM_STATE: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"global:toggle_system_state");
    pub const BUY_SOL: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"global:buy_sol");
    pub const GET_CONVERSION_RATE: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"global:get_conversion_rate");
    pub const DEQUEUE_FILLS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"global:dequeue_fills");
}

impl BorshDeserialize for SolConversionInstructionData {
    fn deserialize_reader<R: io::Read>(reader: &mut R) -> std::io::Result<Self> {
        match Discriminator::deserialize_reader(reader)? {
            Self::INITIALIZE_SYSTEM => {
                let oracle_key = BorshDeserialize::deserialize_reader(reader)?;
                let fixed_fill_quantity_lamports = BorshDeserialize::deserialize_reader(reader)?;
                let price_maximum_age_seconds = BorshDeserialize::deserialize_reader(reader)?;
                let coefficient = BorshDeserialize::deserialize_reader(reader)?;
                let max_discount_rate = BorshDeserialize::deserialize_reader(reader)?;
                let min_discount_rate = BorshDeserialize::deserialize_reader(reader)?;

                Ok(Self::InitializeSystem {
                    oracle_key,
                    fixed_fill_quantity_lamports,
                    price_maximum_age_seconds,
                    coefficient,
                    max_discount_rate,
                    min_discount_rate,
                })
            }
            Self::UPDATE_CONFIGURATION_REGISTRY => {
                let oracle_key = BorshDeserialize::deserialize_reader(reader)?;
                let fixed_fill_quantity_lamports = BorshDeserialize::deserialize_reader(reader)?;
                let price_maximum_age_seconds = BorshDeserialize::deserialize_reader(reader)?;
                let coefficient = BorshDeserialize::deserialize_reader(reader)?;
                let max_discount_rate = BorshDeserialize::deserialize_reader(reader)?;
                let min_discount_rate = BorshDeserialize::deserialize_reader(reader)?;

                Ok(Self::UpdateConfigurationRegistry {
                    oracle_key,
                    fixed_fill_quantity_lamports,
                    price_maximum_age_seconds,
                    coefficient,
                    max_discount_rate,
                    min_discount_rate,
                })
            }
            Self::SET_FILLS_CONSUMER => {
                BorshDeserialize::deserialize_reader(reader).map(Self::SetFillsConsumer)
            }
            Self::ADD_TO_DENY_LIST => Ok(Self::AddToDenyList),
            Self::REMOVE_FROM_DENY_LIST => Ok(Self::RemoveFromDenyList),
            Self::SET_ADMIN => BorshDeserialize::deserialize_reader(reader).map(Self::SetAdmin),
            Self::SET_DENY_LIST_AUTHORITY => Ok(Self::SetDenyListAuthority),
            Self::TOGGLE_SYSTEM_STATE => {
                BorshDeserialize::deserialize_reader(reader).map(Self::ToggleSystemState)
            }
            Self::BUY_SOL => {
                let limit_price = BorshDeserialize::deserialize_reader(reader)?;
                let oracle_price_data = BorshDeserialize::deserialize_reader(reader)?;

                Ok(Self::BuySol {
                    limit_price,
                    oracle_price_data,
                })
            }
            Self::GET_CONVERSION_RATE => Ok(Self::GetConversionRate),
            Self::DEQUEUE_FILLS => Ok(Self::DequeueFills),
            _ => Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "Invalid discriminator",
            )),
        }
    }
}

impl BorshSerialize for SolConversionInstructionData {
    fn serialize<W: io::Write>(&self, writer: &mut W) -> io::Result<()> {
        match self {
            Self::InitializeSystem {
                oracle_key,
                fixed_fill_quantity_lamports,
                price_maximum_age_seconds,
                coefficient,
                max_discount_rate,
                min_discount_rate,
            } => {
                Self::INITIALIZE_SYSTEM.serialize(writer)?;
                oracle_key.serialize(writer)?;
                fixed_fill_quantity_lamports.serialize(writer)?;
                price_maximum_age_seconds.serialize(writer)?;
                coefficient.serialize(writer)?;
                max_discount_rate.serialize(writer)?;
                min_discount_rate.serialize(writer)
            }
            Self::UpdateConfigurationRegistry {
                oracle_key,
                fixed_fill_quantity_lamports,
                price_maximum_age_seconds,
                coefficient,
                max_discount_rate,
                min_discount_rate,
            } => {
                Self::UPDATE_CONFIGURATION_REGISTRY.serialize(writer)?;
                oracle_key.serialize(writer)?;
                fixed_fill_quantity_lamports.serialize(writer)?;
                price_maximum_age_seconds.serialize(writer)?;
                coefficient.serialize(writer)?;
                max_discount_rate.serialize(writer)?;
                min_discount_rate.serialize(writer)
            }
            Self::SetFillsConsumer(fills_consumer_key) => {
                Self::SET_FILLS_CONSUMER.serialize(writer)?;
                fills_consumer_key.serialize(writer)
            }
            Self::AddToDenyList => Self::ADD_TO_DENY_LIST.serialize(writer),
            Self::RemoveFromDenyList => Self::REMOVE_FROM_DENY_LIST.serialize(writer),
            Self::SetAdmin(admin_key) => {
                Self::SET_ADMIN.serialize(writer)?;
                admin_key.serialize(writer)
            }
            Self::SetDenyListAuthority => Self::SET_DENY_LIST_AUTHORITY.serialize(writer),
            Self::ToggleSystemState(should_pause) => {
                Self::TOGGLE_SYSTEM_STATE.serialize(writer)?;
                should_pause.serialize(writer)
            }
            Self::BuySol {
                limit_price,
                oracle_price_data,
            } => {
                Self::BUY_SOL.serialize(writer)?;
                limit_price.serialize(writer)?;
                oracle_price_data.serialize(writer)
            }
            Self::GetConversionRate => Self::GET_CONVERSION_RATE.serialize(writer),
            Self::DequeueFills => Self::DEQUEUE_FILLS.serialize(writer),
        }
    }
}
