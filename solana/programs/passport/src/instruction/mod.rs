pub mod account;

//

use std::io;

use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_tools::{Discriminator, DISCRIMINATOR_LEN};
use solana_pubkey::Pubkey;

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub enum ProgramConfiguration {
    Flag(ProgramFlagConfiguration),
    DoubleZeroLedgerSentinel(Pubkey),
    AccessRequestDeposit {
        request_deposit_lamports: u64,
        request_fee_lamports: u64,
    },
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub enum ProgramFlagConfiguration {
    IsPaused(bool),
    IsRequestAccessPaused(bool),
}

#[derive(Debug, Clone, BorshSerialize, BorshDeserialize, PartialEq, Eq)]
pub enum AccessMode {
    SolanaValidator {
        validator_id: Pubkey,
        service_key: Pubkey,
        ed25519_signature: [u8; 64],
    },
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PassportInstructionData {
    InitializeProgram,
    SetAdmin(Pubkey),
    ConfigureProgram(ProgramConfiguration),
    RequestAccess(AccessMode),
    GrantAccess,
    DenyAccess,
}

impl PassportInstructionData {
    pub const INITIALIZE_PROGRAM: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::initialize_program");
    pub const SET_ADMIN: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::set_admin");
    pub const CONFIGURE_PROGRAM: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::configure_program");
    pub const REQUEST_ACCESS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::request_access");
    pub const GRANT_ACCESS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::grant_access");
    pub const DENY_ACCESS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new_sha2(b"dz::ix::deny_access");
}

impl BorshDeserialize for PassportInstructionData {
    fn deserialize_reader<R: io::Read>(reader: &mut R) -> std::io::Result<Self> {
        match Discriminator::deserialize_reader(reader)? {
            Self::INITIALIZE_PROGRAM => Ok(Self::InitializeProgram),
            Self::SET_ADMIN => BorshDeserialize::deserialize_reader(reader).map(Self::SetAdmin),
            Self::CONFIGURE_PROGRAM => {
                BorshDeserialize::deserialize_reader(reader).map(Self::ConfigureProgram)
            }
            Self::REQUEST_ACCESS => {
                BorshDeserialize::deserialize_reader(reader).map(Self::RequestAccess)
            }
            Self::GRANT_ACCESS => Ok(Self::GrantAccess),
            Self::DENY_ACCESS => Ok(Self::DenyAccess),
            _ => Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "Invalid discriminator",
            )),
        }
    }
}

impl BorshSerialize for PassportInstructionData {
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
            Self::RequestAccess(access_mode) => {
                Self::REQUEST_ACCESS.serialize(writer)?;
                access_mode.serialize(writer)
            }
            Self::GrantAccess => Self::GRANT_ACCESS.serialize(writer),
            Self::DenyAccess => Self::DENY_ACCESS.serialize(writer),
        }
    }
}
