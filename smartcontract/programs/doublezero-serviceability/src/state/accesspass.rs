use crate::state::accounttype::{AccountType, AccountTypeInfo};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, program_error::ProgramError,
    pubkey::Pubkey,
};
use std::{fmt, net::Ipv4Addr};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Default, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum AccessPassType {
    #[default]
    Prepaid,
    SolanaValidator(Pubkey),
}

impl AccessPassType {
    pub fn to_discriminant_string(&self) -> String {
        match self {
            AccessPassType::Prepaid => "prepaid".to_string(),
            AccessPassType::SolanaValidator(_) => "solana_validator".to_string(),
        }
    }
}

impl fmt::Display for AccessPassType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            AccessPassType::Prepaid => write!(f, "prepaid"),
            AccessPassType::SolanaValidator(node_id) => write!(f, "solana_validator: {node_id}"),
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
}

impl From<u8> for AccessPassStatus {
    fn from(value: u8) -> Self {
        match value {
            0 => AccessPassStatus::Requested,
            1 => AccessPassStatus::Connected,
            2 => AccessPassStatus::Disconnected,
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
        }
    }
}

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
}

impl fmt::Display for AccessPass {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self.accesspass_type {
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
        }
    }
}

impl AccountTypeInfo for AccessPass {
    fn seed(&self) -> &[u8] {
        crate::seeds::SEED_ACCESS_PASS
    }
    fn size(&self) -> usize {
        // This operation is safe because we will never overflow usize.
        borsh::object_length(self).unwrap()
    }
    fn bump_seed(&self) -> u8 {
        self.bump_seed
    }
    fn index(&self) -> u128 {
        0 // AccessPass does not have an index like other accounts
    }
    fn owner(&self) -> Pubkey {
        self.owner
    }
}

impl From<&[u8]> for AccessPass {
    fn from(mut data: &[u8]) -> Self {
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
        };

        assert_eq!(
            out.account_type,
            AccountType::AccessPass,
            "Invalid AccessPass Account Type"
        );

        out
    }
}

impl TryFrom<&AccountInfo<'_>> for AccessPass {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        Ok(Self::from(&data[..]))
    }
}

impl AccessPass {
    pub fn try_serialize(&self, account: &AccountInfo) -> ProgramResult {
        let mut data = &mut account.data.borrow_mut()[..];
        self.serialize(&mut data)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_device_serialization() {
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
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = AccessPass::from(&data[..]);

        assert_eq!(val.size(), val2.size());
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.bump_seed, val2.bump_seed);
        assert_eq!(val.accesspass_type, val2.accesspass_type);
        assert_eq!(val.client_ip, val2.client_ip);
        assert_eq!(val.user_payer, val2.user_payer);
        assert_eq!(val.last_access_epoch, val2.last_access_epoch);
        assert_eq!(val.connection_count, val2.connection_count);
        assert_eq!(val.status, val2.status);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }
}
