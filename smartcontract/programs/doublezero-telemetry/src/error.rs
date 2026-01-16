use solana_program::program_error::ProgramError;
use std::fmt;

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum TelemetryError {
    /// Agent is not authorized to write telemetry for this device
    UnauthorizedAgent = 1001,
    /// Device is not in activate or suspended status
    DeviceNotActivated = 1002,
    /// Link is not in activate or suspended status
    LinkNotActivated = 1003,
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
    /// Invalid sampling interval
    InvalidSamplingInterval = 1012,
    /// Samples batch too large
    SamplesBatchTooLarge = 1013,
    /// Exchange is not activated or suspend
    ExchangeNotActiveOrSuspended = 1014,
    /// Date provider name is greater than 32 bytes
    DataProviderNameTooLong = 1015,
    /// Origin and target exchanges cannot be the
    SameTargetAsOrigin = 1016,
    /// Write transaction contains no samples
    EmptyLatencySamples = 1017,
}

impl From<TelemetryError> for ProgramError {
    fn from(e: TelemetryError) -> Self {
        ProgramError::Custom(e as u32)
    }
}

impl fmt::Display for TelemetryError {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self {
            Self::UnauthorizedAgent => write!(
                f,
                "Agent is not authorized to write telemetry for this device"
            ),
            Self::DeviceNotActivated => {
                write!(f, "Device is not activated")
            }
            Self::LinkNotActivated => {
                write!(f, "Link is not activated")
            }
            Self::InvalidLink => write!(f, "Link does not connect the specified devices"),
            Self::EpochMismatch => {
                write!(f, "Epoch mismatch between account and instruction")
            }
            Self::SamplesAccountFull => write!(f, "Samples account is full"),
            Self::InvalidAccountType => write!(f, "Invalid account type"),
            Self::InvalidAccountOwner => write!(f, "Account owner mismatch"),
            Self::InvalidPDA => write!(f, "Invalid PDA"),
            Self::AccountAlreadyExists => write!(f, "Account already exists"),
            Self::AccountDoesNotExist => write!(f, "Account does not exist"),
            Self::InvalidSamplingInterval => write!(f, "Invalid sampling interval"),
            Self::SamplesBatchTooLarge => {
                write!(f, "Samples batch too large")
            }
            Self::ExchangeNotActiveOrSuspended => {
                write!(f, "Exchange does not have activated status")
            }
            Self::DataProviderNameTooLong => write!(f, "Data provider name exceeds 32 bytes"),
            Self::SameTargetAsOrigin => write!(f, "Origin and target are the same exchange"),
            Self::EmptyLatencySamples => write!(f, "Write transaction contains no samples"),
        }
    }
}
