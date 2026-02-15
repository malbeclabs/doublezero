use solana_program::program_error::ProgramError;
use thiserror::Error;

#[derive(Debug, Error, PartialEq, Clone)]
pub enum GeolocationError {
    #[error("Custom program error: {0:#x}")]
    Custom(u32),
    #[error("Invalid account type")]
    InvalidAccountType,
    #[error("Not allowed")]
    NotAllowed,
    #[error("Invalid code length (max 32 bytes)")]
    InvalidCodeLength,
    #[error("Invalid IP address: not publicly routable")]
    InvalidIpAddress,
    #[error("Maximum parent devices reached")]
    MaxParentDevicesReached,
    #[error("Parent device not found")]
    ParentDeviceNotFound,
    #[error("Invalid serviceability program ID")]
    InvalidServiceabilityProgramId,
    #[error("Invalid account code")]
    InvalidAccountCode,
    #[error("Parent device already exists")]
    ParentDeviceAlreadyExists,
    #[error("Reference count is not zero")]
    ReferenceCountNotZero,
    #[error("Unauthorized: payer is not the upgrade authority")]
    UnauthorizedInitializer,
}

impl From<GeolocationError> for ProgramError {
    fn from(e: GeolocationError) -> Self {
        match e {
            GeolocationError::Custom(e) => ProgramError::Custom(e),
            GeolocationError::InvalidAccountType => ProgramError::Custom(1),
            GeolocationError::NotAllowed => ProgramError::Custom(2),
            GeolocationError::InvalidCodeLength => ProgramError::Custom(4),
            GeolocationError::InvalidIpAddress => ProgramError::Custom(5),
            GeolocationError::MaxParentDevicesReached => ProgramError::Custom(6),
            GeolocationError::ParentDeviceNotFound => ProgramError::Custom(8),
            GeolocationError::InvalidServiceabilityProgramId => ProgramError::Custom(11),
            GeolocationError::InvalidAccountCode => ProgramError::Custom(12),
            GeolocationError::ParentDeviceAlreadyExists => ProgramError::Custom(13),
            GeolocationError::ReferenceCountNotZero => ProgramError::Custom(15),
            GeolocationError::UnauthorizedInitializer => ProgramError::Custom(17),
        }
    }
}

impl From<u32> for GeolocationError {
    fn from(e: u32) -> Self {
        match e {
            1 => GeolocationError::InvalidAccountType,
            2 => GeolocationError::NotAllowed,
            4 => GeolocationError::InvalidCodeLength,
            5 => GeolocationError::InvalidIpAddress,
            6 => GeolocationError::MaxParentDevicesReached,
            8 => GeolocationError::ParentDeviceNotFound,
            11 => GeolocationError::InvalidServiceabilityProgramId,
            12 => GeolocationError::InvalidAccountCode,
            13 => GeolocationError::ParentDeviceAlreadyExists,
            15 => GeolocationError::ReferenceCountNotZero,
            17 => GeolocationError::UnauthorizedInitializer,
            _ => GeolocationError::Custom(e),
        }
    }
}

impl From<ProgramError> for GeolocationError {
    fn from(e: ProgramError) -> Self {
        match e {
            ProgramError::Custom(e) => e.into(),
            _ => GeolocationError::Custom(0),
        }
    }
}

pub trait Validate {
    fn validate(&self) -> Result<(), GeolocationError>;
}

#[cfg(test)]
mod tests {
    use super::*;

    fn all_named_variants() -> Vec<(GeolocationError, u32)> {
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
        ]
    }

    #[test]
    fn test_round_trip_named_variants() {
        for (variant, expected_code) in all_named_variants() {
            let program_error: ProgramError = variant.clone().into();
            let ProgramError::Custom(code) = program_error else {
                panic!("expected ProgramError::Custom for {:?}", variant);
            };
            assert_eq!(
                code, expected_code,
                "variant {:?} should map to code {}",
                variant, expected_code
            );

            let round_tripped: GeolocationError = program_error.into();
            assert_eq!(
                round_tripped, variant,
                "round-trip failed for code {}",
                expected_code
            );
        }
    }

    #[test]
    fn test_custom_variant_round_trip() {
        let original = GeolocationError::Custom(999);
        let program_error: ProgramError = original.clone().into();
        let round_tripped: GeolocationError = program_error.into();
        assert_eq!(round_tripped, GeolocationError::Custom(999));
    }

    #[test]
    fn test_custom_zero_round_trip() {
        let original = GeolocationError::Custom(0);
        let program_error: ProgramError = original.clone().into();
        let round_tripped: GeolocationError = program_error.into();
        assert_eq!(round_tripped, GeolocationError::Custom(0));
    }

    #[test]
    fn test_unknown_u32_becomes_custom() {
        let error: GeolocationError = 42u32.into();
        assert_eq!(error, GeolocationError::Custom(42));
    }

    #[test]
    fn test_non_custom_program_error_becomes_custom_zero() {
        let error: GeolocationError = ProgramError::InvalidArgument.into();
        assert_eq!(error, GeolocationError::Custom(0));
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
        assert_eq!(
            GeolocationError::Custom(0x1234).to_string(),
            "Custom program error: 0x1234"
        );
    }
}
