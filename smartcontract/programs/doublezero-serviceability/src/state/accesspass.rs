use crate::{
    error::{DoubleZeroError, Validate},
    helper::deserialize_vec_with_capacity,
    state::accounttype::AccountType,
};

use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::AccountInfo, clock::Clock, entrypoint::ProgramResult, msg,
    program_error::ProgramError, pubkey::Pubkey, sysvar::Sysvar,
};
use std::{fmt, net::Ipv4Addr};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Default, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum AccessPassType {
    #[default]
    Prepaid,
    SolanaValidator(
        #[cfg_attr(
            feature = "serde",
            serde(
                serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
                deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
            )
        )]
        Pubkey,
    ),
    SolanaRPC(
        #[cfg_attr(
            feature = "serde",
            serde(
                serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
                deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
            )
        )]
        Pubkey,
    ),
    SolanaMulticastPublisher(
        #[cfg_attr(
            feature = "serde",
            serde(
                serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
                deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
            )
        )]
        Pubkey,
    ),
    SolanaMulticastSubscriber(
        #[cfg_attr(
            feature = "serde",
            serde(
                serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
                deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
            )
        )]
        Pubkey,
    ),
    Others(String, String), // (type_name, key)
}

impl AccessPassType {
    pub fn to_discriminant_string(&self) -> String {
        match self {
            AccessPassType::Prepaid => "prepaid".to_string(),
            AccessPassType::SolanaValidator(_) => "solana_validator".to_string(),
            AccessPassType::SolanaRPC(_) => "solana_rpc".to_string(),
            AccessPassType::SolanaMulticastPublisher(_) => "solana_multicast_publisher".to_string(),
            AccessPassType::SolanaMulticastSubscriber(_) => {
                "solana_multicast_subscriber".to_string()
            }
            AccessPassType::Others(type_name, _) => type_name.clone(),
        }
    }
}

impl fmt::Display for AccessPassType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            AccessPassType::Prepaid => write!(f, "prepaid"),
            AccessPassType::SolanaValidator(node_id) => write!(f, "solana_validator: {node_id}"),
            AccessPassType::SolanaRPC(node_id) => write!(f, "solana_rpc: {node_id}"),
            AccessPassType::SolanaMulticastPublisher(node_id) => {
                write!(f, "solana_multicast_publisher: {node_id}")
            }
            AccessPassType::SolanaMulticastSubscriber(node_id) => {
                write!(f, "solana_multicast_subscriber: {node_id}")
            }
            AccessPassType::Others(type_name, key) => {
                write!(f, "others: {} ({})", type_name, key)
            }
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Default, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum AccessPassStatus {
    #[default]
    Requested = 0,
    Connected = 1,
    Disconnected = 2,
    Expired = 3,
}

impl From<u8> for AccessPassStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => AccessPassStatus::Requested,
            1 => AccessPassStatus::Connected,
            2 => AccessPassStatus::Disconnected,
            3 => AccessPassStatus::Expired,
            _ => AccessPassStatus::Requested,
        }
    }
}

impl fmt::Display for AccessPassStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            AccessPassStatus::Requested => write!(f, "requested"),
            AccessPassStatus::Connected => write!(f, "connected"),
            AccessPassStatus::Disconnected => write!(f, "disconnected"),
            AccessPassStatus::Expired => write!(f, "expired"),
        }
    }
}

impl Validate for AccessPass {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        if self.account_type != AccountType::AccessPass {
            msg!("Invalid account type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }
        self.accesspass_type.validate()?;
        Ok(())
    }
}

impl Validate for AccessPassType {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        match self {
            AccessPassType::SolanaValidator(solana_identity) => {
                if *solana_identity == Pubkey::default() {
                    msg!("Invalid Solana Validator Pubkey: {}", solana_identity);
                    return Err(DoubleZeroError::InvalidSolanaPubkey);
                }
                Ok(())
            }
            AccessPassType::SolanaRPC(solana_identity) => {
                if *solana_identity == Pubkey::default() {
                    msg!("Invalid Solana RPC Pubkey: {}", solana_identity);
                    return Err(DoubleZeroError::InvalidSolanaPubkey);
                }
                Ok(())
            }
            AccessPassType::SolanaMulticastPublisher(solana_identity) => {
                if *solana_identity == Pubkey::default() {
                    msg!(
                        "Invalid Solana Multicast Publisher Pubkey: {}",
                        solana_identity
                    );
                    return Err(DoubleZeroError::InvalidSolanaPubkey);
                }
                Ok(())
            }
            AccessPassType::SolanaMulticastSubscriber(solana_identity) => {
                if *solana_identity == Pubkey::default() {
                    msg!(
                        "Invalid Solana Multicast Subscriber Pubkey: {}",
                        solana_identity
                    );
                    return Err(DoubleZeroError::InvalidSolanaPubkey);
                }
                Ok(())
            }
            _ => Ok(()),
        }
    }
}

pub const IS_DYNAMIC: u8 = 1 << 0; // 0000_0001
pub const ALLOW_MULTIPLE_IP: u8 = 1 << 1; // 0000_0010

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct AccessPass {
    pub account_type: AccountType, // 1
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub owner: Pubkey, // 32
    pub bump_seed: u8,             // 1
    pub accesspass_type: AccessPassType, // 1 or 33
    pub client_ip: Ipv4Addr,       // 4
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string",
            deserialize_with = "doublezero_program_common::serializer::deserialize_pubkey_from_string"
        )
    )]
    pub user_payer: Pubkey, // 32
    pub last_access_epoch: u64,    // 8 / 0-Rejected / u64::MAX unlimited
    pub connection_count: u16,     // 2
    pub status: AccessPassStatus,  // 1
    pub mgroup_pub_allowlist: Vec<Pubkey>, // Vec<32> - List of multicast groups this AccessPass can publish to
    pub mgroup_sub_allowlist: Vec<Pubkey>, // Vec<32> - List of multicast groups this AccessPass can subscribe to
    pub flags: u8,                         // 1
}

impl fmt::Display for AccessPass {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match &self.accesspass_type {
            AccessPassType::Prepaid => {
                if self.last_access_epoch == u64::MAX {
                    write!(f, "Prepaid: (MAX)")
                } else {
                    write!(f, "Prepaid: (expires epoch {})", self.last_access_epoch)
                }
            }
            AccessPassType::SolanaValidator(node_id) => {
                write!(f, "SolanaValidator: ({node_id})")
            }
            AccessPassType::SolanaRPC(node_id) => {
                write!(f, "SolanaRPC: ({node_id})")
            }
            AccessPassType::SolanaMulticastPublisher(node_id) => {
                write!(f, "SolanaMulticastPublisher: ({node_id})")
            }
            AccessPassType::SolanaMulticastSubscriber(node_id) => {
                write!(f, "SolanaMulticastSubscriber: ({node_id})")
            }
            AccessPassType::Others(type_name, details) => {
                write!(f, "Others: {} ({})", type_name, details)
            }
        }
    }
}

impl TryFrom<&[u8]> for AccessPass {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            accesspass_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            client_ip: BorshDeserialize::deserialize(&mut data)
                .unwrap_or(std::net::Ipv4Addr::UNSPECIFIED),
            user_payer: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            last_access_epoch: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            connection_count: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            status: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            mgroup_pub_allowlist: deserialize_vec_with_capacity(&mut data).unwrap_or_default(),
            mgroup_sub_allowlist: deserialize_vec_with_capacity(&mut data).unwrap_or_default(),
            flags: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::AccessPass {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for AccessPass {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!("Failed to deserialize AccessPass: {:?}", res.as_ref().err());
        }
        res
    }
}

impl AccessPass {
    pub fn update_status(&mut self) -> ProgramResult {
        let clock = Clock::get()?;
        let mut current_epoch = clock.epoch;

        // Ensure current_epoch is never zero
        if current_epoch == 0 {
            current_epoch = 1;
        }

        self.status = if self.last_access_epoch < current_epoch {
            AccessPassStatus::Expired
        } else if self.connection_count > 0 {
            AccessPassStatus::Connected
        } else {
            AccessPassStatus::Requested
        };

        Ok(())
    }

    pub fn is_dynamic(&self) -> bool {
        (self.flags & IS_DYNAMIC) != 0
    }
    pub fn allow_multiple_ip(&self) -> bool {
        (self.flags & ALLOW_MULTIPLE_IP) != 0
    }
    pub fn flags_string(&self) -> String {
        let mut flags = Vec::new();
        if self.is_dynamic() {
            flags.push("dynamic");
        }
        if self.allow_multiple_ip() {
            flags.push("allow_multiple_ip");
        }
        flags.join(", ")
    }
}

#[cfg(test)]
mod tests {
    use borsh::object_length;

    use super::*;

    #[test]
    fn test_state_compatibility_accesspass() {
        /* To generate the base64 strings, use the following commands after deploying the program and creating accounts:

        solana account <pubkey> --output json  -u  https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16

         */
        let versions = ["C7qqHuIng+xr+jC+xdH+K0McWbY0Sz2o800JnFlfiUXD/wEKMn8sVpVpD9hZLrMBs5vmoZJrEr3Jm/+Bnz0ZNxH2ctRTKz4oucUt7JbWjAqOf/dn7tAFvWQRcKAJn5fTUPSytlyzaP//////////AAAC",
        "Cw/Yc23gE4TGWMth7/U6RH8eyKMyCwPgaOt85q71G6p0/QFzjyfb7L+DkaP2MshouP9HaAlv5WdMR67oUvQEuw1uStGfmm5o6ww5SH/2KjjMhIvhn7m0SqBUWZF0hnxc/wZ9cXaKdP//////////AQAB",
        "C7qqHuIng+xr+jC+xdH+K0McWbY0Sz2o800JnFlfiUXD/gBD1XgJuqoe4ieD7Gv6ML7F0f4rQxxZtjRLPajzTQmcWV+JRcP//////////wEAAQAAAAAAAAAAAA=="];

        crate::helper::base_tests::test_parsing::<AccessPass>(&versions).unwrap();
    }

    #[test]
    fn test_state_accesspass_types() {
        let a = AccessPassType::Prepaid;
        assert_eq!(object_length(&a).unwrap(), 1);

        let b = AccessPassType::SolanaValidator(Pubkey::default());
        assert_eq!(object_length(&b).unwrap(), 33);
    }

    #[test]
    fn test_state_accesspass_prepaid_serialization() {
        let val = AccessPass {
            account_type: AccountType::AccessPass,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: [1, 2, 3, 4].into(),
            user_payer: Pubkey::new_unique(),
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            flags: 0,
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = AccessPass::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(
            borsh::object_length(&val).unwrap(),
            borsh::object_length(&val2).unwrap()
        );
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.bump_seed, val2.bump_seed);
        assert_eq!(val.accesspass_type, val2.accesspass_type);
        assert_eq!(val.client_ip, val2.client_ip);
        assert_eq!(val.user_payer, val2.user_payer);
        assert_eq!(val.last_access_epoch, val2.last_access_epoch);
        assert_eq!(val.connection_count, val2.connection_count);
        assert_eq!(val.status, val2.status);
        assert_eq!(val.flags, val2.flags);
        assert_eq!(
            data.len(),
            borsh::object_length(&val).unwrap(),
            "Invalid Size"
        );
    }

    #[test]
    fn test_state_accesspass_solana_validator_serialization() {
        let val = AccessPass {
            account_type: AccountType::AccessPass,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            accesspass_type: AccessPassType::SolanaValidator(Pubkey::new_unique()),
            client_ip: [1, 2, 3, 4].into(),
            user_payer: Pubkey::new_unique(),
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            flags: 0,
        };

        let data = borsh::to_vec(&val).unwrap();
        let len = data.len();
        let val2 = AccessPass::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(borsh::object_length(&val).unwrap(), len, "Invalid Size");
        assert_eq!(len, borsh::object_length(&val2).unwrap(), "Invalid Size");
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.bump_seed, val2.bump_seed);
        assert_eq!(val.accesspass_type, val2.accesspass_type);
        assert_eq!(val.client_ip, val2.client_ip);
        assert_eq!(val.user_payer, val2.user_payer);
        assert_eq!(val.last_access_epoch, val2.last_access_epoch);
        assert_eq!(val.connection_count, val2.connection_count);
        assert_eq!(val.flags, val2.flags);
        assert_eq!(val.status, val2.status);
    }

    #[test]
    fn test_state_accesspass_try_from_defaults() {
        let data = [AccountType::AccessPass as u8];
        let val = AccessPass::try_from(&data[..]).unwrap();

        assert_eq!(val.owner, Pubkey::default());
        assert_eq!(val.bump_seed, 0);
        assert_eq!(val.accesspass_type, AccessPassType::default());
        assert_eq!(val.client_ip, Ipv4Addr::new(0, 0, 0, 0));
        assert_eq!(val.user_payer, Pubkey::default());
        assert_eq!(val.last_access_epoch, 0);
        assert_eq!(val.connection_count, 0);
        assert_eq!(val.flags, 0);
        assert_eq!(val.status, AccessPassStatus::default());
    }

    #[test]
    fn test_state_accesspass_solana_validator_serialization_overflow() {
        let val = AccessPass {
            account_type: AccountType::AccessPass,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            accesspass_type: AccessPassType::SolanaValidator(Pubkey::new_unique()),
            client_ip: [1, 2, 3, 4].into(),
            user_payer: Pubkey::new_unique(),
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            flags: 0,
        };

        let mut data = borsh::to_vec(&val).unwrap();
        let len = data.len();
        data.push(0);
        let val2 = AccessPass::try_from(&data[..]).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(borsh::object_length(&val).unwrap(), len, "Invalid Size");
        assert_eq!(len, borsh::object_length(&val2).unwrap(), "Invalid Size");
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.bump_seed, val2.bump_seed);
        assert_eq!(val.accesspass_type, val2.accesspass_type);
        assert_eq!(val.client_ip, val2.client_ip);
        assert_eq!(val.user_payer, val2.user_payer);
        assert_eq!(val.last_access_epoch, val2.last_access_epoch);
        assert_eq!(val.connection_count, val2.connection_count);
        assert_eq!(val.flags, val2.flags);
        assert_eq!(val.status, val2.status);
    }

    #[test]
    fn test_state_accesspass_validate_error_invalid_account_type() {
        let val = AccessPass {
            account_type: AccountType::Device, // Should be AccessPass
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: [1, 2, 3, 4].into(),
            user_payer: Pubkey::new_unique(),
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            flags: 0,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidAccountType);
    }

    #[test]
    fn test_state_accesspass_validate_error_invalid_solana_validator_pubkey() {
        let val = AccessPass {
            account_type: AccountType::AccessPass,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            accesspass_type: AccessPassType::SolanaValidator(Pubkey::default()), // Invalid
            client_ip: [1, 2, 3, 4].into(),
            user_payer: Pubkey::new_unique(),
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            flags: 0,
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidSolanaPubkey);
    }
}
