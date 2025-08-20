use crate::{
    bytereader::ByteReader,
    state::accounttype::{AccountType, AccountTypeInfo},
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, program_error::ProgramError,
    pubkey::Pubkey,
};
use std::{fmt, net::Ipv4Addr};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum AccessPassType {
    Prepaid = 0,
    SolanaValidator = 1,
}

impl From<u8> for AccessPassType {
    fn from(value: u8) -> Self {
        match value {
            0 => AccessPassType::Prepaid,
            1 => AccessPassType::SolanaValidator,
            _ => AccessPassType::Prepaid, // Default case
        }
    }
}

impl From<String> for AccessPassType {
    fn from(value: String) -> Self {
        match value.to_ascii_lowercase().as_str() {
            "prepaid" => AccessPassType::Prepaid,
            "solanavalidator" => AccessPassType::SolanaValidator,
            _ => AccessPassType::Prepaid, // Default case
        }
    }
}

impl fmt::Display for AccessPassType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            AccessPassType::Prepaid => write!(f, "prepaid"),
            AccessPassType::SolanaValidator => write!(f, "solanavalidator"),
        }
    }
}

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Debug, Copy, Clone, PartialEq)]
#[borsh(use_discriminant = true)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub enum AccessPassStatus {
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
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string"
        )
    )]
    pub owner: Pubkey, // 32
    pub bump_seed: u8,             // 1
    pub accesspass_type: AccessPassType, // 1
    pub client_ip: Ipv4Addr,       // 4
    #[cfg_attr(
        feature = "serde",
        serde(
            serialize_with = "doublezero_program_common::serializer::serialize_pubkey_as_string"
        )
    )]
    pub payer: Pubkey, // 32
    pub last_access_epoch: u64,    // 8 / 0-Rejected / u64::MAX unlimited
    pub connection_count: u16,     // 2
    pub status: AccessPassStatus,  // 1
}

impl fmt::Display for AccessPass {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, ip: {}, bump_seed: {}, accesspass_type: {}, payer: {}, last_access_epoch: {}, status: {}",
            self.account_type, self.owner, self.client_ip, self.bump_seed, self.accesspass_type, self.payer, self.last_access_epoch, self.status
        )
    }
}

impl AccountTypeInfo for AccessPass {
    fn seed(&self) -> &[u8] {
        crate::seeds::SEED_ACCESS_PASS
    }
    fn size(&self) -> usize {
        1 + 32 + 1 + 1 + 4 + 32 + 8 + 2 + 1
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
    fn from(data: &[u8]) -> Self {
        let mut parser = ByteReader::new(data);

        let out = Self {
            account_type: parser.read_enum(),
            owner: parser.read_pubkey(),
            bump_seed: parser.read_u8(),
            accesspass_type: parser.read_enum(),
            client_ip: parser.read_ipv4(),
            payer: parser.read_pubkey(),
            last_access_epoch: parser.read_u64(),
            connection_count: parser.read_u16(),
            status: parser.read_enum(),
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
            payer: Pubkey::new_unique(),
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
        assert_eq!(val.payer, val2.payer);
        assert_eq!(val.last_access_epoch, val2.last_access_epoch);
        assert_eq!(val.connection_count, val2.connection_count);
        assert_eq!(val.status, val2.status);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }
}
