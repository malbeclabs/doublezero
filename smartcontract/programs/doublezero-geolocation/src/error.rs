use solana_program::program_error::ProgramError;
use thiserror::Error;

#[derive(Debug, Error, PartialEq, Clone)]
#[repr(u32)]
pub enum GeolocationError {
    #[error("Invalid account type")]
    InvalidAccountType = 1,
    #[error("Not allowed")]
    NotAllowed = 2,
    #[error("Invalid code length (max 32 bytes)")]
    InvalidCodeLength = 4,
    #[error("Invalid IP address: not publicly routable")]
    InvalidIpAddress = 5,
    #[error("Maximum parent devices reached")]
    MaxParentDevicesReached = 6,
    #[error("Parent device not found")]
    ParentDeviceNotFound = 8,
    #[error("Invalid serviceability program ID")]
    InvalidServiceabilityProgramId = 11,
    #[error("Invalid account code")]
    InvalidAccountCode = 12,
    #[error("Parent device already exists")]
    ParentDeviceAlreadyExists = 13,
    #[error("Reference count is not zero")]
    ReferenceCountNotZero = 15,
    #[error("Unauthorized: payer is not the upgrade authority")]
    UnauthorizedInitializer = 17,
    #[error("min_compatible_version cannot exceed version")]
    InvalidMinCompatibleVersion = 18,
}

impl From<GeolocationError> for ProgramError {
    fn from(e: GeolocationError) -> Self {
        ProgramError::Custom(e as u32)
    }
}

pub trait Validate {
    fn validate(&self) -> Result<(), GeolocationError>;
}

#[cfg(test)]
mod tests {
    use super::*;

    fn all_variants() -> Vec<(GeolocationError, u32)> {
        vec![
            (GeolocationError::InvalidAccountType, 1),
            (GeolocationError::NotAllowed, 2),
            (GeolocationError::InvalidCodeLength, 4),
            (GeolocationError::InvalidIpAddress, 5),
            (GeolocationError::MaxParentDevicesReached, 6),
            (GeolocationError::ParentDeviceNotFound, 8),
            (GeolocationError::InvalidServiceabilityProgramId, 11),
            (GeolocationError::InvalidAccountCode, 12),
            (GeolocationError::ParentDeviceAlreadyExists, 13),
            (GeolocationError::ReferenceCountNotZero, 15),
            (GeolocationError::UnauthorizedInitializer, 17),
            (GeolocationError::InvalidMinCompatibleVersion, 18),
        ]
    }

    #[test]
    fn test_error_codes() {
        for (variant, expected_code) in all_variants() {
            let program_error: ProgramError = variant.clone().into();
            let ProgramError::Custom(code) = program_error else {
                panic!("expected ProgramError::Custom for {:?}", variant);
            };
            assert_eq!(
                code, expected_code,
                "variant {:?} should map to code {}",
                variant, expected_code
            );
        }
    }

    #[test]
    fn test_error_display_messages() {
        assert_eq!(
            GeolocationError::InvalidAccountType.to_string(),
            "Invalid account type"
        );
        assert_eq!(GeolocationError::NotAllowed.to_string(), "Not allowed");
        assert_eq!(
            GeolocationError::InvalidIpAddress.to_string(),
            "Invalid IP address: not publicly routable"
        );
    }
}
