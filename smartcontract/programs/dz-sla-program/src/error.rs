use solana_program::program_error::ProgramError;
use thiserror::Error;

#[derive(Debug, Error)]
pub enum DoubleZeroError {
    #[error("Custom program error: {0:#x}")]
    Custom(u32), // variant 0
    #[error("Only the owner can perform this action")]
    InvalidOwnerPubkey, // variant 1
    #[error("You are trying to assign a Pubkey that does not correspond to a Exchange")]
    InvalidExchangePubkey, // variant 2
    #[error("You are trying to assign a Pubkey that does not correspond to a Device")]
    InvalidDevicePubkey, // variant 3
    #[error("You are trying to assign a Pubkey that does not correspond to a Location")]
    InvalidLocationPubkey, // variant 4
    #[error("You are trying to assign a Pubkey that does not correspond to a Device A")]
    InvalidDeviceAPubkey, // variant 5
    #[error("You are trying to assign a Pubkey that does not correspond to a Device Z")]
    InvalidDeviceZPubkey, // variant 6
    #[error("Invalid Status")]
    InvalidStatus, // variant 7
    #[error("You are not allowed to execute this action")]
    NotAllowed, // variant 8
}

impl From<DoubleZeroError> for ProgramError {
    fn from(e: DoubleZeroError) -> Self {
        match e {
            DoubleZeroError::Custom(e) => ProgramError::Custom(e),
            DoubleZeroError::InvalidOwnerPubkey => ProgramError::Custom(1),
            DoubleZeroError::InvalidLocationPubkey => ProgramError::Custom(2),
            DoubleZeroError::InvalidExchangePubkey => ProgramError::Custom(3),
            DoubleZeroError::InvalidDeviceAPubkey => ProgramError::Custom(4),
            DoubleZeroError::InvalidDeviceZPubkey => ProgramError::Custom(5),
            DoubleZeroError::InvalidDevicePubkey => ProgramError::Custom(6),
            DoubleZeroError::InvalidStatus => ProgramError::Custom(7),
            DoubleZeroError::NotAllowed => ProgramError::Custom(8),
        }
    }
}

impl From<ProgramError> for DoubleZeroError {
    fn from(e: ProgramError) -> Self {
        match e {
            ProgramError::Custom(e) => match e {
                1 => DoubleZeroError::InvalidOwnerPubkey,
                2 => DoubleZeroError::InvalidLocationPubkey,
                3 => DoubleZeroError::InvalidExchangePubkey,
                4 => DoubleZeroError::InvalidDeviceAPubkey,
                5 => DoubleZeroError::InvalidDeviceZPubkey,
                6 => DoubleZeroError::InvalidDevicePubkey,
                7 => DoubleZeroError::InvalidStatus,
                8 => DoubleZeroError::NotAllowed,
                _ => DoubleZeroError::Custom(e),
            },
            _ => DoubleZeroError::Custom(0),
        }
    }
}
