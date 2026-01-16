use crate::{
    error::{DoubleZeroError, Validate},
    helper::{deserialize_vec_with_capacity, is_global},
    state::{
        accesspass::{AccessPass, AccessPassStatus, AccessPassType},
        accounttype::AccountType,
    },
};
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::types::NetworkV4;
use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, msg, program_error::ProgramError,
    pubkey::Pubkey,
};
use std::{fmt, net::Ipv4Addr};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum UserType {
    #[default]
    IBRL = 0,
    IBRLWithAllocatedIP = 1,
    EdgeFiltering = 2,
    Multicast = 3,
}

impl From<u8> for UserType {
    fn from(value: u8) -> Self {
        match value {
            0 => UserType::IBRL,
            1 => UserType::IBRLWithAllocatedIP,
            2 => UserType::EdgeFiltering,
            3 => UserType::Multicast,
            // TODO: leaving this as it unwinds a lot of things and doesn't seem worth the effort at this time
            _ => panic!("Unknown UserType"),
        }
    }
}

impl fmt::Display for UserType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            UserType::IBRL => write!(f, "IBRL"),
            UserType::IBRLWithAllocatedIP => write!(f, "IBRLWithAllocatedIP"),
            UserType::EdgeFiltering => write!(f, "EdgeFiltering"),
            UserType::Multicast => write!(f, "Multicast"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum UserCYOA {
    #[default]
    None = 0,
    GREOverDIA = 1,
    GREOverFabric = 2,
    GREOverPrivatePeering = 3,
    GREOverPublicPeering = 4,
    GREOverCable = 5,
}

impl From<u8> for UserCYOA {
    fn from(value: u8) -> Self {
        match value {
            1 => UserCYOA::GREOverDIA,
            2 => UserCYOA::GREOverFabric,
            3 => UserCYOA::GREOverPrivatePeering,
            4 => UserCYOA::GREOverPublicPeering,
            5 => UserCYOA::GREOverCable,
            _ => UserCYOA::None,
        }
    }
}

impl fmt::Display for UserCYOA {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            UserCYOA::None => write!(f, "none"),
            UserCYOA::GREOverDIA => write!(f, "GREOverDIA"),
            UserCYOA::GREOverFabric => write!(f, "GREOverFabric"),
            UserCYOA::GREOverPrivatePeering => write!(f, "GREOverPrivatePeering"),
            UserCYOA::GREOverPublicPeering => write!(f, "GREOverPublicPeering"),
            UserCYOA::GREOverCable => write!(f, "GREOverCable"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq, Default)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum UserStatus {
    #[default]
    Pending = 0,
    Activated = 1,
    SuspendedDeprecated = 2, // deprecated
    Deleting = 3,
    Rejected = 4,
    PendingBan = 5,
    Banned = 6,
    Updating = 7,
    OutOfCredits = 8,
}

impl From<u8> for UserStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => UserStatus::Pending,
            1 => UserStatus::Activated,
            2 => UserStatus::SuspendedDeprecated,
            3 => UserStatus::Deleting,
            4 => UserStatus::Rejected,
            5 => UserStatus::PendingBan,
            6 => UserStatus::Banned,
            7 => UserStatus::Updating,
            8 => UserStatus::OutOfCredits,
            _ => UserStatus::Pending,
        }
    }
}

impl fmt::Display for UserStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            UserStatus::Pending => write!(f, "pending"),
            UserStatus::Activated => write!(f, "activated"),
            UserStatus::SuspendedDeprecated => write!(f, "suspended"),
            UserStatus::Deleting => write!(f, "deleting"),
            UserStatus::Rejected => write!(f, "rejected"),
            UserStatus::PendingBan => write!(f, "pending ban"),
            UserStatus::Updating => write!(f, "updating"),
            UserStatus::Banned => write!(f, "banned"),
            UserStatus::OutOfCredits => write!(f, "out_of_credits"),
        }
    }
}

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct User {
    pub account_type: AccountType, // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub owner: Pubkey, // 32
    pub index: u128,               // 16
    pub bump_seed: u8,             // 1
    pub user_type: UserType,       // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub tenant_pk: Pubkey, // 32
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub device_pk: Pubkey, // 32
    pub cyoa_type: UserCYOA,       // 1
    pub client_ip: Ipv4Addr,       // 4
    pub dz_ip: Ipv4Addr,           // 4
    pub tunnel_id: u16,            // 2
    pub tunnel_net: NetworkV4,     // 5
    pub status: UserStatus,        // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkeylist_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkeylist_from_string"
        )
    )]
    pub publishers: Vec<Pubkey>, // 4 + 32 * len
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkeylist_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkeylist_from_string"
        )
    )]
    pub subscribers: Vec<Pubkey>, // 4 + 32 * len
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub validator_pubkey: Pubkey, // 32
}

impl fmt::Display for User {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, index: {}, user_type: {}, device_pk: {}, cyoa_type: {}, client_ip: {}, dz_ip: {}, tunnel_id: {}, tunnel_net: {}, status: {}",
            self.account_type,
            self.owner,
            self.index,
            self.user_type,
            self.device_pk,
            self.cyoa_type,
            &self.client_ip,
            &self.dz_ip,
            self.tunnel_id,
            &self.tunnel_net,
            self.status
        )
    }
}

impl TryFrom<&[u8]> for User {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, ProgramError> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            index: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            user_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            tenant_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            device_pk: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            cyoa_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            client_ip: BorshDeserialize::deserialize(&mut data).unwrap_or([0, 0, 0, 0].into()),
            dz_ip: BorshDeserialize::deserialize(&mut data).unwrap_or([0, 0, 0, 0].into()),
            tunnel_id: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            tunnel_net: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            status: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            publishers: deserialize_vec_with_capacity(&mut data).unwrap_or_default(),
            subscribers: deserialize_vec_with_capacity(&mut data).unwrap_or_default(),
            validator_pubkey: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::User {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for User {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!("Failed to deserialize User: {:?}", res.as_ref().err());
        }
        res
    }
}

impl Validate for User {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        // Account type must be User
        if self.account_type != AccountType::User {
            msg!("account_type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }
        // Device public key must be valid
        if self.device_pk == Pubkey::default() {
            msg!("device_pk: {}", self.device_pk);
            return Err(DoubleZeroError::InvalidDevicePubkey);
        }
        // client_ip must be global unicast
        if !is_global(self.client_ip) {
            msg!("client_ip: {}", self.client_ip);
            return Err(DoubleZeroError::InvalidClientIp);
        }
        // dz_ip must be global unicast
        if self.status != UserStatus::Pending && !is_global(self.dz_ip) {
            msg!("dz_ip: {}", self.dz_ip);
            return Err(DoubleZeroError::InvalidDzIp);
        }
        // tunnel net must be private
        if self.status != UserStatus::Pending && !self.tunnel_net.ip().is_link_local() {
            msg!("tunnel_net: {}", self.tunnel_net);
            return Err(DoubleZeroError::InvalidTunnelNet);
        }
        // tunnel_id must be less than or equal to 1024
        if self.tunnel_id > 1024 {
            msg!("tunnel_id: {}", self.tunnel_id);
            return Err(DoubleZeroError::InvalidTunnelId);
        }

        Ok(())
    }
}

impl User {
    pub fn get_multicast_groups(&self) -> Vec<Pubkey> {
        let mut groups: Vec<Pubkey> = vec![];

        // Add publishers first
        groups.extend(self.publishers.iter().cloned());

        // Add subscribers that aren't already in the list
        for sub in &self.subscribers {
            if !groups.contains(sub) {
                groups.push(*sub);
            }
        }

        groups
    }

    pub fn try_activate(&mut self, accesspass: &mut AccessPass) -> ProgramResult {
        accesspass.update_status()?;

        self.validator_pubkey = match &accesspass.accesspass_type {
            AccessPassType::SolanaValidator(pk) => *pk,
            _ => Pubkey::default(),
        };

        self.status = if accesspass.status == AccessPassStatus::Expired {
            UserStatus::OutOfCredits
        } else {
            UserStatus::Activated
        };

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_compatibility_user() {
        /* To generate the base64 strings, use the following commands after deploying the program and creating accounts:

        solana account <Pubkey> --output json  -u  https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16

         */
        let versions = ["B6gVJ9nqZZCbOZ4+qdSD0fV6GW608QGIxlc96bI9/o1ukAMAAAAAAAAAAAAAAAAAAP8AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABNn75kIGVAa79vzDXmsfzMjJv6k6bA4q/3il4oq4agjAFfrc7uX63O7i8Cqf4CxB8BAAAAAAAAAACjpUHKupgvsyUs0s3LR1ojd7zOQkjFxGsDoH+BeOYVWw==",
        "B7qqHuIng+xr+jC+xdH+K0McWbY0Sz2o800JnFlfiUXDTBgAAAAAAAAAAAAAAAAAAP4AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABNn75kIGVAa79vzDXmsfzMjJv6k6bA4q/3il4oq4agjAFD1XgJQ9V4CQMCqf4ApB8BAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="];

        crate::helper::base_tests::test_parsing::<User>(&versions).unwrap();
    }

    #[test]
    fn test_state_user_try_from_defaults() {
        let data = [AccountType::User as u8];
        let val = User::try_from(&data[..]).unwrap();

        assert_eq!(val.owner, Pubkey::default());
        assert_eq!(val.bump_seed, 0);
        assert_eq!(val.index, 0);
        assert_eq!(val.user_type, UserType::IBRL);
        assert_eq!(val.device_pk, Pubkey::default());
        assert_eq!(val.cyoa_type, UserCYOA::None);
        assert_eq!(val.client_ip, Ipv4Addr::new(0, 0, 0, 0));
        assert_eq!(val.dz_ip, Ipv4Addr::new(0, 0, 0, 0));
        assert_eq!(val.tunnel_id, 0);
        assert_eq!(
            val.tunnel_net,
            NetworkV4::new(Ipv4Addr::new(0, 0, 0, 0), 0).unwrap()
        );
        assert_eq!(val.status, UserStatus::Pending);
        assert_eq!(val.publishers, Vec::<Pubkey>::new());
        assert_eq!(val.subscribers, Vec::<Pubkey>::new());
        assert_eq!(val.validator_pubkey, Pubkey::default());
    }

    #[test]
    fn test_state_user_serialization() {
        let val = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::GREOverDIA,
            dz_ip: [3, 2, 4, 2].into(),
            client_ip: [1, 2, 3, 4].into(),
            tunnel_id: 0,
            tunnel_net: "169.254.0.0/25".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            subscribers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            validator_pubkey: Pubkey::new_unique(),
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = User::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(
            borsh::object_length(&val).unwrap(),
            borsh::object_length(&val2).unwrap()
        );
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.device_pk, val2.device_pk);
        assert_eq!(val.dz_ip, val2.dz_ip);
        assert_eq!(val.client_ip, val2.client_ip);
        assert_eq!(val.tunnel_net, val2.tunnel_net);
        assert_eq!(val.subscribers, val2.subscribers);
        assert_eq!(val.publishers, val2.publishers);
        assert_eq!(val.validator_pubkey, val2.validator_pubkey);
        assert_eq!(
            data.len(),
            borsh::object_length(&val).unwrap(),
            "Invalid Size"
        );
    }

    #[test]
    fn test_state_user_validate_error_invalid_dz_ip() {
        let val = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::GREOverDIA,
            dz_ip: [0, 0, 0, 0].into(),
            client_ip: [1, 2, 3, 4].into(),
            tunnel_id: 0,
            tunnel_net: "10.0.0.1/25".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            subscribers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            validator_pubkey: Pubkey::new_unique(),
        };

        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidDzIp);
    }

    #[test]
    fn test_state_user_validate_error_invalid_account_type() {
        let val = User {
            account_type: AccountType::AccessPass, // Not User
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::GREOverDIA,
            dz_ip: [3, 2, 4, 2].into(),
            client_ip: [1, 2, 3, 4].into(),
            tunnel_id: 0,
            tunnel_net: "10.0.0.1/25".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            subscribers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            validator_pubkey: Pubkey::new_unique(),
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidAccountType);
    }

    #[test]
    fn test_state_user_validate_error_invalid_device_pubkey() {
        let val = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            device_pk: Pubkey::default(), // Invalid
            cyoa_type: UserCYOA::GREOverDIA,
            dz_ip: [3, 2, 4, 2].into(),
            client_ip: [1, 2, 3, 4].into(),
            tunnel_id: 0,
            tunnel_net: "10.0.0.1/25".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            subscribers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            validator_pubkey: Pubkey::new_unique(),
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidDevicePubkey);
    }

    #[test]
    fn test_state_user_validate_error_invalid_client_ip() {
        let val = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::GREOverDIA,
            dz_ip: [3, 2, 4, 2].into(),
            client_ip: [0, 0, 0, 0].into(), // Invalid
            tunnel_id: 0,
            tunnel_net: "10.0.0.1/25".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            subscribers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            validator_pubkey: Pubkey::new_unique(),
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidClientIp);
    }

    #[test]
    fn test_state_user_validate_error_invalid_tunnel_net() {
        let val = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::GREOverDIA,
            dz_ip: [3, 2, 4, 2].into(),
            client_ip: [1, 2, 3, 4].into(),
            tunnel_id: 0,
            tunnel_net: "8.8.8.8/25".parse().unwrap(), // Not link-local
            status: UserStatus::Activated,
            publishers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            subscribers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            validator_pubkey: Pubkey::new_unique(),
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidTunnelNet);
    }

    #[test]
    fn test_state_user_validate_error_invalid_tunnel_id() {
        let val = User {
            account_type: AccountType::User,
            owner: Pubkey::new_unique(),
            index: 123,
            bump_seed: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::GREOverDIA,
            dz_ip: [3, 2, 4, 2].into(),
            client_ip: [1, 2, 3, 4].into(),
            tunnel_id: 2048, // Invalid
            tunnel_net: "169.254.0.0/25".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            subscribers: vec![Pubkey::new_unique(), Pubkey::new_unique()],
            validator_pubkey: Pubkey::new_unique(),
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidTunnelId);
    }
}
