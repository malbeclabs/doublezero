use solana_program::program_error::ProgramError;
use std::fmt;

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum TelemetryError {
    /// Agent is not authorized to write telemetry for this device
    UnauthorizedAgent = 1001,
    /// Device is not in activated status
    DeviceNotActive = 1002,
    /// Link is not in activated status
    LinkNotActive = 1003,
    /// Link does not connect the specified devices
    InvalidLink = 1004,
    /// Epoch mismatch between account and instruction
    EpochMismatch = 1005,
    /// Samples account is full
    SamplesAccountFull = 1006,
    /// Invalid account type
    InvalidAccountType = 1007,
    /// Account owner mismatch
    InvalidAccountOwner = 1008,
    /// Invalid PDA
    InvalidPDA = 1009,
    /// Account already exists
    AccountAlreadyExists = 1010,
    /// Account does not exist
    AccountDoesNotExist = 1011,
}

impl From<TelemetryError> for ProgramError {
    fn from(e: TelemetryError) -> Self {
        ProgramError::Custom(e as u32)
    }
}

impl fmt::Display for TelemetryError {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self {
            TelemetryError::UnauthorizedAgent => write!(
                f,
                "Agent is not authorized to write telemetry for this device"
            ),
            TelemetryError::DeviceNotActive => write!(f, "Device is not in activated status"),
            TelemetryError::LinkNotActive => write!(f, "Link is not in activated status"),
            TelemetryError::InvalidLink => write!(f, "Link does not connect the specified devices"),
            TelemetryError::EpochMismatch => {
                write!(f, "Epoch mismatch between account and instruction")
            }
            TelemetryError::SamplesAccountFull => write!(f, "Samples account is full"),
            TelemetryError::InvalidAccountType => write!(f, "Invalid account type"),
            TelemetryError::InvalidAccountOwner => write!(f, "Account owner mismatch"),
            TelemetryError::InvalidPDA => write!(f, "Invalid PDA"),
            TelemetryError::AccountAlreadyExists => write!(f, "Account already exists"),
            TelemetryError::AccountDoesNotExist => write!(f, "Account does not exist"),
        }
    }
}
