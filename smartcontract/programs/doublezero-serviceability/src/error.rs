use solana_program::program_error::ProgramError;
use thiserror::Error;

#[derive(Debug, Error, PartialEq, Clone)]
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
    #[error("Invalid Account Type")]
    InvalidAccountType, // variant 9
    #[error("You are trying to assign a Pubkey that does not correspond to a Contributor")]
    InvalidContributorPubkey, // variant 10
    #[error("Invalid Interface Version")]
    InvalidInterfaceVersion, // variant 11
    #[error("Invalid Interface Name")]
    InvalidInterfaceName, // variant 12
    #[error("Reference Count is not zero")]
    ReferenceCountNotZero, // variant 13
    #[error("Invalid Contributor")]
    InvalidContributor, // variant 14
    #[error("Invalid External Link: Side Z interface name should be empty")]
    InvalidInterfaceZForExternal, // variant 15
    #[error("Invalid index")]
    InvalidIndex, // variant 16
    #[error("Device already set")]
    DeviceAlreadySet, // variant 17
    #[error("Device not set")]
    DeviceNotSet, // variant 18
    #[error("Invalid account code")]
    InvalidAccountCode, // variant 19
    #[error("Max users exceeded")]
    MaxUsersExceeded, // variant 20
    #[error("Invalid last access epoch")]
    InvalidLastAccessEpoch, // variant 21
    #[error("Unauthorized")]
    Unauthorized, // variant 22
    #[error("Invalid Solana Validator Pubkey")]
    InvalidSolanaPubkey, // variant 23
    #[error("InterfaceNotFound")]
    InterfaceNotFound, // variant 24
    #[error("Invalid Access Pass")]
    AccessPassUnauthorized, // variant 25
    #[error("Invalid Client IP")]
    InvalidClientIp, // variant 26
    #[error("Invalid DZ IP")]
    InvalidDzIp, // variant 27
    #[error("Invalid Tunnel Network")]
    InvalidTunnelNet, // variant 28
    #[error("Invalid Tunnel ID")]
    InvalidTunnelId, // variant 29
    #[error("Invalid Tunnel IP")]
    InvalidTunnelIp, // variant 30
    #[error("Invalid Bandwidth")]
    InvalidBandwidth, // variant 31
    #[error("Invalid Delay")]
    InvalidDelay, // variant 32
    #[error("Invalid Jitter")]
    InvalidJitter, // variant 33
    #[error("Code too long")]
    CodeTooLong, // variant 34
    #[error("No DZ Prefixes")]
    NoDzPrefixes, // variant 35
    #[error("Invalid Location")]
    InvalidLocation, // variant 36
    #[error("Invalid Exchange")]
    InvalidExchange, // variant 37
    #[error("Invalid DZ Prefix")]
    InvalidDzPrefix, // variant 38
    #[error("Name too long")]
    NameTooLong, // variant 39
    #[error("Invalid Latitude")]
    InvalidLatitude, // variant 40
    #[error("Invalid Longitude")]
    InvalidLongitude, // variant 41
    #[error("Invalid Location ID")]
    InvalidLocId, // variant 42
    #[error("Invalid Country Code")]
    InvalidCountryCode, // variant 43
    #[error("Invalid Local ASN")]
    InvalidLocalAsn, // variant 44
    #[error("Invalid Remote ASN")]
    InvalidRemoteAsn, // variant 45
    #[error("Invalid MTU")]
    InvalidMtu, // variant 46
    #[error("Invalid Interface IP")]
    InvalidInterfaceIp, // variant 47
    #[error("Invalid Interface IP Net")]
    InvalidInterfaceIpNet, // variant 48
    #[error("Invalid VLAN ID")]
    InvalidVlanId, // variant 49
    #[error("Invalid Max Bandwidth")]
    InvalidMaxBandwidth, // variant 50
    #[error("Invalid Multicast IP")]
    InvalidMulticastIp, // variant 51
    #[error("Invalid Account Owner")]
    InvalidAccountOwner, // variant 52
    #[error("Access Pass not found")]
    AccessPassNotFound, // variant 53
    #[error("User account not found")]
    UserAccountNotFound, // variant 54
    #[error("Invalid BGP Community")]
    InvalidBgpCommunity, // variant 55
    #[error("Interface already exists")]
    InterfaceAlreadyExists, // variant 56
    #[error("Invalid Interface Type")]
    InvalidInterfaceType, // variant 57
    #[error("Invalid Loopback Type")]
    InvalidLoopbackType, // variant 58
    #[error("Invalid Minimum Compatible Version")]
    InvalidMinCompatibleVersion, // variant 59
    #[error("Invalid Actual Location")]
    InvalidActualLocation, // variant 60
    #[error("Invalid User Pubkey")]
    InvalidUserPubkey, // variant 61
    #[error("Invalid Public IP: IP conflicts with DZ prefix")]
    InvalidPublicIp, // variant 62
    #[error("Allocation failed, resource exhausted")]
    AllocationFailed, // variant 63
    #[error("Serialization failed")]
    SerializationFailure, // variant 64
    #[error("Invalid argument")]
    InvalidArgument, // variant 65
    #[error("Invalid Foundation Allowlist: cannot be empty")]
    InvalidFoundationAllowlist, // variant 66
    #[error("Deprecated error")]
    Deprecated, // variant 67
    #[error("Immutable Field")]
    ImmutableField, // variant 68
    #[error("CYOA can only be set on physical interfaces")]
    CyoaRequiresPhysical, // variant 69
    #[error("Device can only be removed if it has no interfaces")]
    DeviceHasInterfaces, // variant 70
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
            DoubleZeroError::InvalidAccountType => ProgramError::Custom(9),
            DoubleZeroError::InvalidContributorPubkey => ProgramError::Custom(10),
            DoubleZeroError::InvalidInterfaceVersion => ProgramError::Custom(11),
            DoubleZeroError::InvalidInterfaceName => ProgramError::Custom(12),
            DoubleZeroError::ReferenceCountNotZero => ProgramError::Custom(13),
            DoubleZeroError::InvalidContributor => ProgramError::Custom(14),
            DoubleZeroError::InvalidInterfaceZForExternal => ProgramError::Custom(15),
            DoubleZeroError::InvalidIndex => ProgramError::Custom(16),
            DoubleZeroError::DeviceAlreadySet => ProgramError::Custom(17),
            DoubleZeroError::DeviceNotSet => ProgramError::Custom(18),
            DoubleZeroError::InvalidAccountCode => ProgramError::Custom(19),
            DoubleZeroError::MaxUsersExceeded => ProgramError::Custom(20),
            DoubleZeroError::InvalidLastAccessEpoch => ProgramError::Custom(21),
            DoubleZeroError::Unauthorized => ProgramError::Custom(22),
            DoubleZeroError::InvalidSolanaPubkey => ProgramError::Custom(23),
            DoubleZeroError::InterfaceNotFound => ProgramError::Custom(24),
            DoubleZeroError::AccessPassUnauthorized => ProgramError::Custom(25),
            DoubleZeroError::InvalidClientIp => ProgramError::Custom(26),
            DoubleZeroError::InvalidDzIp => ProgramError::Custom(27),
            DoubleZeroError::InvalidTunnelNet => ProgramError::Custom(28),
            DoubleZeroError::InvalidTunnelId => ProgramError::Custom(29),
            DoubleZeroError::InvalidTunnelIp => ProgramError::Custom(30),
            DoubleZeroError::InvalidBandwidth => ProgramError::Custom(31),
            DoubleZeroError::InvalidDelay => ProgramError::Custom(32),
            DoubleZeroError::InvalidJitter => ProgramError::Custom(33),
            DoubleZeroError::CodeTooLong => ProgramError::Custom(34),
            DoubleZeroError::NoDzPrefixes => ProgramError::Custom(35),
            DoubleZeroError::InvalidLocation => ProgramError::Custom(36),
            DoubleZeroError::InvalidExchange => ProgramError::Custom(37),
            DoubleZeroError::InvalidDzPrefix => ProgramError::Custom(38),
            DoubleZeroError::NameTooLong => ProgramError::Custom(39),
            DoubleZeroError::InvalidLatitude => ProgramError::Custom(40),
            DoubleZeroError::InvalidLongitude => ProgramError::Custom(41),
            DoubleZeroError::InvalidLocId => ProgramError::Custom(42),
            DoubleZeroError::InvalidCountryCode => ProgramError::Custom(43),
            DoubleZeroError::InvalidLocalAsn => ProgramError::Custom(44),
            DoubleZeroError::InvalidRemoteAsn => ProgramError::Custom(45),
            DoubleZeroError::InvalidMtu => ProgramError::Custom(46),
            DoubleZeroError::InvalidInterfaceIp => ProgramError::Custom(47),
            DoubleZeroError::InvalidInterfaceIpNet => ProgramError::Custom(48),
            DoubleZeroError::InvalidVlanId => ProgramError::Custom(49),
            DoubleZeroError::InvalidMaxBandwidth => ProgramError::Custom(50),
            DoubleZeroError::InvalidMulticastIp => ProgramError::Custom(51),
            DoubleZeroError::InvalidAccountOwner => ProgramError::Custom(52),
            DoubleZeroError::AccessPassNotFound => ProgramError::Custom(53),
            DoubleZeroError::UserAccountNotFound => ProgramError::Custom(54),
            DoubleZeroError::InvalidBgpCommunity => ProgramError::Custom(55),
            DoubleZeroError::InterfaceAlreadyExists => ProgramError::Custom(56),
            DoubleZeroError::InvalidInterfaceType => ProgramError::Custom(57),
            DoubleZeroError::InvalidLoopbackType => ProgramError::Custom(58),
            DoubleZeroError::InvalidMinCompatibleVersion => ProgramError::Custom(59),
            DoubleZeroError::InvalidActualLocation => ProgramError::Custom(60),
            DoubleZeroError::InvalidUserPubkey => ProgramError::Custom(61),
            DoubleZeroError::InvalidPublicIp => ProgramError::Custom(62),
            DoubleZeroError::AllocationFailed => ProgramError::Custom(63),
            DoubleZeroError::SerializationFailure => ProgramError::Custom(64),
            DoubleZeroError::InvalidArgument => ProgramError::Custom(65),
            DoubleZeroError::InvalidFoundationAllowlist => ProgramError::Custom(66),
            DoubleZeroError::Deprecated => ProgramError::Custom(67),
            DoubleZeroError::ImmutableField => ProgramError::Custom(68),
            DoubleZeroError::CyoaRequiresPhysical => ProgramError::Custom(69),
            DoubleZeroError::DeviceHasInterfaces => ProgramError::Custom(70),
        }
    }
}

impl From<u32> for DoubleZeroError {
    fn from(e: u32) -> Self {
        match e {
            1 => DoubleZeroError::InvalidOwnerPubkey,
            2 => DoubleZeroError::InvalidLocationPubkey,
            3 => DoubleZeroError::InvalidExchangePubkey,
            4 => DoubleZeroError::InvalidDeviceAPubkey,
            5 => DoubleZeroError::InvalidDeviceZPubkey,
            6 => DoubleZeroError::InvalidDevicePubkey,
            7 => DoubleZeroError::InvalidStatus,
            8 => DoubleZeroError::NotAllowed,
            9 => DoubleZeroError::InvalidAccountType,
            10 => DoubleZeroError::InvalidContributorPubkey,
            11 => DoubleZeroError::InvalidInterfaceVersion,
            12 => DoubleZeroError::InvalidInterfaceName,
            13 => DoubleZeroError::ReferenceCountNotZero,
            14 => DoubleZeroError::InvalidContributor,
            15 => DoubleZeroError::InvalidInterfaceZForExternal,
            16 => DoubleZeroError::InvalidIndex,
            17 => DoubleZeroError::DeviceAlreadySet,
            18 => DoubleZeroError::DeviceNotSet,
            19 => DoubleZeroError::InvalidAccountCode,
            20 => DoubleZeroError::MaxUsersExceeded,
            21 => DoubleZeroError::InvalidLastAccessEpoch,
            22 => DoubleZeroError::Unauthorized,
            23 => DoubleZeroError::InvalidSolanaPubkey,
            24 => DoubleZeroError::InterfaceNotFound,
            25 => DoubleZeroError::AccessPassUnauthorized,
            26 => DoubleZeroError::InvalidClientIp,
            27 => DoubleZeroError::InvalidDzIp,
            28 => DoubleZeroError::InvalidTunnelNet,
            29 => DoubleZeroError::InvalidTunnelId,
            30 => DoubleZeroError::InvalidTunnelIp,
            31 => DoubleZeroError::InvalidBandwidth,
            32 => DoubleZeroError::InvalidDelay,
            33 => DoubleZeroError::InvalidJitter,
            34 => DoubleZeroError::CodeTooLong,
            35 => DoubleZeroError::NoDzPrefixes,
            36 => DoubleZeroError::InvalidLocation,
            37 => DoubleZeroError::InvalidExchange,
            38 => DoubleZeroError::InvalidDzPrefix,
            39 => DoubleZeroError::NameTooLong,
            40 => DoubleZeroError::InvalidLatitude,
            41 => DoubleZeroError::InvalidLongitude,
            42 => DoubleZeroError::InvalidLocId,
            43 => DoubleZeroError::InvalidCountryCode,
            44 => DoubleZeroError::InvalidLocalAsn,
            45 => DoubleZeroError::InvalidRemoteAsn,
            46 => DoubleZeroError::InvalidMtu,
            47 => DoubleZeroError::InvalidInterfaceIp,
            48 => DoubleZeroError::InvalidInterfaceIpNet,
            49 => DoubleZeroError::InvalidVlanId,
            50 => DoubleZeroError::InvalidMaxBandwidth,
            51 => DoubleZeroError::InvalidMulticastIp,
            52 => DoubleZeroError::InvalidAccountOwner,
            53 => DoubleZeroError::AccessPassNotFound,
            54 => DoubleZeroError::UserAccountNotFound,
            55 => DoubleZeroError::InvalidBgpCommunity,
            56 => DoubleZeroError::InterfaceAlreadyExists,
            57 => DoubleZeroError::InvalidInterfaceType,
            58 => DoubleZeroError::InvalidLoopbackType,
            59 => DoubleZeroError::InvalidMinCompatibleVersion,
            60 => DoubleZeroError::InvalidActualLocation,
            61 => DoubleZeroError::InvalidUserPubkey,
            62 => DoubleZeroError::InvalidPublicIp,
            63 => DoubleZeroError::AllocationFailed,
            64 => DoubleZeroError::SerializationFailure,
            65 => DoubleZeroError::InvalidArgument,
            66 => DoubleZeroError::InvalidFoundationAllowlist,
            67 => DoubleZeroError::Deprecated,
            68 => DoubleZeroError::ImmutableField,
            69 => DoubleZeroError::CyoaRequiresPhysical,
            70 => DoubleZeroError::DeviceHasInterfaces,
            _ => DoubleZeroError::Custom(e),
        }
    }
}

impl From<ProgramError> for DoubleZeroError {
    fn from(e: ProgramError) -> Self {
        match e {
            ProgramError::Custom(e) => e.into(),
            _ => DoubleZeroError::Custom(0),
        }
    }
}

pub trait Validate {
    fn validate(&self) -> Result<(), DoubleZeroError>;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_error_enum_conversions() {
        use DoubleZeroError::*;
        let variants = vec![
            Custom(123),
            InvalidOwnerPubkey,
            InvalidExchangePubkey,
            InvalidDevicePubkey,
            InvalidLocationPubkey,
            InvalidDeviceAPubkey,
            InvalidDeviceZPubkey,
            InvalidStatus,
            NotAllowed,
            InvalidAccountType,
            InvalidContributorPubkey,
            InvalidInterfaceVersion,
            InvalidInterfaceName,
            ReferenceCountNotZero,
            InvalidContributor,
            InvalidInterfaceZForExternal,
            InvalidIndex,
            DeviceAlreadySet,
            DeviceNotSet,
            InvalidAccountCode,
            MaxUsersExceeded,
            InvalidLastAccessEpoch,
            Unauthorized,
            InvalidSolanaPubkey,
            InterfaceNotFound,
            AccessPassUnauthorized,
            InvalidClientIp,
            InvalidDzIp,
            InvalidTunnelNet,
            InvalidTunnelId,
            InvalidTunnelIp,
            InvalidBandwidth,
            InvalidDelay,
            InvalidJitter,
            CodeTooLong,
            NoDzPrefixes,
            InvalidLocation,
            InvalidExchange,
            InvalidDzPrefix,
            NameTooLong,
            InvalidLatitude,
            InvalidLongitude,
            InvalidLocId,
            InvalidCountryCode,
            InvalidLocalAsn,
            InvalidRemoteAsn,
            InvalidMtu,
            InvalidInterfaceIp,
            InvalidInterfaceIpNet,
            InvalidVlanId,
            InvalidMaxBandwidth,
            InvalidMulticastIp,
            InvalidAccountOwner,
            AccessPassNotFound,
            UserAccountNotFound,
            InvalidBgpCommunity,
            InterfaceAlreadyExists,
            InvalidInterfaceType,
            InvalidLoopbackType,
            InvalidMinCompatibleVersion,
            InvalidActualLocation,
            InvalidUserPubkey,
            InvalidPublicIp,
            AllocationFailed,
            SerializationFailure,
            InvalidArgument,
            InvalidFoundationAllowlist,
            Deprecated,
            ImmutableField,
            CyoaRequiresPhysical,
            DeviceHasInterfaces,
        ];
        for err in variants {
            let pe: ProgramError = err.clone().into();
            let err2: DoubleZeroError = pe.into();
            assert_eq!(err, err2, "Error conversion failed for {err:?}");
        }
    }
}
