use crate::{ip_allocator::IpAllocator, state::accounttype::AccountType};
use doublezero_program_common::types::NetworkV4;

use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, program_error::ProgramError, pubkey::Pubkey};
use std::{fmt, io::Cursor};

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Clone, Debug, PartialEq)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
#[borsh(use_discriminant = true)]
pub enum ResourceExtensionType {
    Ip(IpAllocator),
}

#[derive(BorshSerialize, BorshDeserialize, Clone, Debug, PartialEq)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct ResourceExtensionOwned {
    pub account_type: AccountType,             // 1
    pub owner: Pubkey,                         // 32
    pub bump_seed: u8,                         // 1
    pub assocatiated_with: Pubkey,             // 32
    pub extension_type: ResourceExtensionType, // Variable
    pub storage: Vec<u8>,                      // Variable
}

impl ResourceExtensionOwned {
    pub fn iter_allocated_ips(&self) -> Vec<NetworkV4> {
        match &self.extension_type {
            ResourceExtensionType::Ip(ip_allocator) => {
                ip_allocator.iter_allocated(&self.storage).collect()
            }
        }
    }
}

impl TryFrom<&[u8]> for ResourceExtensionOwned {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            assocatiated_with: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            extension_type: {
                let ext_type: u8 = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
                match ext_type {
                    0 => ResourceExtensionType::Ip(IpAllocator::deserialize(&mut data)?),
                    _ => return Err(ProgramError::InvalidAccountData),
                }
            },
            storage: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::ResourceExtension {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl fmt::Display for ResourceExtensionOwned {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "TODO")
    }
}

pub struct ResourceExtensionBorrowed<'a> {
    pub account_type: AccountType,             // 1
    pub owner: Pubkey,                         // 32
    pub bump_seed: u8,                         // 1
    pub assocatiated_with: Pubkey,             // 32
    pub extension_type: ResourceExtensionType, // Variable
    pub storage: &'a mut [u8],
}

impl<'a> ResourceExtensionBorrowed<'a> {
    pub fn construct_ip_resource(
        account: &AccountInfo,
        owner: Pubkey,
        bump_seed: u8,
        assocatiated_with: Pubkey,
        base_net: NetworkV4,
        allocation_size: u8,
    ) -> Result<(), ProgramError> {
        let mut buffer = account.data.borrow_mut();
        let mut cursor = Cursor::new(&mut buffer[..]);
        let account_type = AccountType::ResourceExtension;
        account_type
            .serialize(&mut cursor)
            .map_err(|_| ProgramError::InvalidAccountData)?;
        owner
            .serialize(&mut cursor)
            .map_err(|_| ProgramError::InvalidAccountData)?;
        bump_seed
            .serialize(&mut cursor)
            .map_err(|_| ProgramError::InvalidAccountData)?;
        assocatiated_with
            .serialize(&mut cursor)
            .map_err(|_| ProgramError::InvalidAccountData)?;
        0u8.serialize(&mut cursor)
            .map_err(|_| ProgramError::InvalidAccountData)?;
        base_net
            .serialize(&mut cursor)
            .map_err(|_| ProgramError::InvalidAccountData)?;
        allocation_size
            .serialize(&mut cursor)
            .map_err(|_| ProgramError::InvalidAccountData)?;

        Ok(())
    }

    pub fn inplace_from(data: &'a mut [u8]) -> Result<Self, ProgramError> {
        let (data, bitmap) = data.split_at_mut(67);
        let mut cursor: &[u8] = data;
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut cursor).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut cursor).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut cursor).unwrap_or_default(),
            assocatiated_with: BorshDeserialize::deserialize(&mut cursor).unwrap_or_default(),
            extension_type: {
                let ext_type: u8 = BorshDeserialize::deserialize(&mut cursor).unwrap_or_default();
                match ext_type {
                    0 => ResourceExtensionType::Ip(IpAllocator::deserialize(&mut cursor)?),
                    _ => return Err(ProgramError::InvalidAccountData),
                }
            },
            storage: bitmap,
        };

        if out.account_type != AccountType::ResourceExtension {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }

    pub fn allocate(&mut self) -> Result<NetworkV4, ProgramError> {
        match &mut self.extension_type {
            ResourceExtensionType::Ip(ip_allocator) => {
                ip_allocator
                    .allocate(self.storage)
                    .ok_or(ProgramError::Custom(0)) // TODO
            }
        }
    }

    pub fn deallocate(&mut self, network: NetworkV4) -> bool {
        match &mut self.extension_type {
            ResourceExtensionType::Ip(ip_allocator) => {
                ip_allocator.deallocate(self.storage, &network)
            }
        }
    }
}

impl<'a> fmt::Display for ResourceExtensionBorrowed<'a> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "ResourceExtensionBorrowed {{ account_type: {:?}, owner: {}, bump_seed: {}, assocatiated_with: {}, extension_type: {:?}, storage_len: {} }}",
            self.account_type,
            self.owner,
            self.bump_seed,
            self.assocatiated_with,
            self.extension_type,
            self.storage.len()
        )
    }
}
