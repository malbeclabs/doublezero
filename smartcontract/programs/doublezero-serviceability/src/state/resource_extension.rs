use crate::{error::DoubleZeroError, ip_allocator::IpAllocator, state::accounttype::AccountType};
use doublezero_program_common::types::NetworkV4;

use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, pubkey::Pubkey};
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
    pub extension_type: ResourceExtensionType, // 9
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
    type Error = DoubleZeroError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            assocatiated_with: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            extension_type: BorshDeserialize::deserialize(&mut data).unwrap(),
            storage: data[4..].to_vec(),
        };

        if out.account_type != AccountType::ResourceExtension {
            return Err(DoubleZeroError::InvalidAccountType);
        }

        Ok(out)
    }
}

impl fmt::Display for ResourceExtensionOwned {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "ResourceExtensionOwned {{ account_type: {:?}, owner: {}, bump_seed: {}, assocatiated_with: {}, extension_type: {:?} }}",
            self.account_type,
            self.owner,
            self.bump_seed,
            self.assocatiated_with,
            self.extension_type,
        )?;

        match &self.extension_type {
            ResourceExtensionType::Ip(ip_allocator) => {
                write!(f, ", allocated_ips: [")?;
                let mut first = true;
                for ip in ip_allocator.iter_allocated(self.storage.as_slice()) {
                    if !first {
                        write!(f, ", ")?;
                    }
                    write!(f, "{}", ip)?;
                    first = false;
                }
                write!(f, "]")?;
            }
        }

        fmt::Result::Ok(())
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
    pub fn size(base_net: &NetworkV4, allocation_size: u32) -> usize {
        // last +4 is to pad out the bitmap to align on u64
        1 + 32 + 1 + 32 + 1 + IpAllocator::size(base_net, allocation_size) + 4
    }

    pub fn construct_ip_resource(
        account: &AccountInfo,
        owner: &Pubkey,
        bump_seed: u8,
        assocatiated_with: &Pubkey,
        base_net: &NetworkV4,
        allocation_size: u32,
    ) -> Result<(), DoubleZeroError> {
        let mut buffer = account.data.borrow_mut();
        let mut cursor = Cursor::new(&mut buffer[..]);
        let account_type = AccountType::ResourceExtension;
        account_type
            .serialize(&mut cursor)
            .map_err(|_| DoubleZeroError::SerializationFailure)?;
        owner
            .serialize(&mut cursor)
            .map_err(|_| DoubleZeroError::SerializationFailure)?;
        bump_seed
            .serialize(&mut cursor)
            .map_err(|_| DoubleZeroError::SerializationFailure)?;
        assocatiated_with
            .serialize(&mut cursor)
            .map_err(|_| DoubleZeroError::SerializationFailure)?;
        ResourceExtensionType::Ip(
            IpAllocator::new(*base_net, allocation_size)
                .map_err(|_| DoubleZeroError::SerializationFailure)?,
        )
        .serialize(&mut cursor)
        .map_err(|_| DoubleZeroError::SerializationFailure)?;

        Ok(())
    }

    pub fn inplace_from(data: &'a mut [u8]) -> Result<Self, DoubleZeroError> {
        let (data, bitmap) = data.split_at_mut(80);
        let mut cursor: &[u8] = data;
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut cursor).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut cursor).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut cursor).unwrap_or_default(),
            assocatiated_with: BorshDeserialize::deserialize(&mut cursor).unwrap_or_default(),
            extension_type: BorshDeserialize::deserialize(&mut cursor).unwrap(),
            storage: bitmap,
        };

        if out.account_type != AccountType::ResourceExtension {
            return Err(DoubleZeroError::InvalidAccountType);
        }

        Ok(out)
    }

    pub fn allocate(&mut self) -> Result<NetworkV4, DoubleZeroError> {
        match &mut self.extension_type {
            ResourceExtensionType::Ip(ip_allocator) => ip_allocator
                .allocate(self.storage)
                .ok_or(DoubleZeroError::AllocationFailed),
        }
    }

    pub fn allocate_specific(&mut self, ip_net: &NetworkV4) -> Result<(), DoubleZeroError> {
        match &mut self.extension_type {
            ResourceExtensionType::Ip(ip_allocator) => ip_allocator
                .allocate_specific(self.storage, ip_net)
                .map_err(|_| DoubleZeroError::AllocationFailed),
        }
    }

    pub fn deallocate(&mut self, network: &NetworkV4) -> bool {
        match &mut self.extension_type {
            ResourceExtensionType::Ip(ip_allocator) => {
                ip_allocator.deallocate(self.storage, network)
            }
        }
    }
}

impl<'a> fmt::Display for ResourceExtensionBorrowed<'a> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "ResourceExtensionBorrowed {{ account_type: {:?}, owner: {}, bump_seed: {}, assocatiated_with: {}, extension_type: {:?} }}",
            self.account_type,
            self.owner,
            self.bump_seed,
            self.assocatiated_with,
            self.extension_type,
        )?;

        match &self.extension_type {
            ResourceExtensionType::Ip(ip_allocator) => {
                write!(f, ", allocated_ips: [")?;
                let mut first = true;
                for ip in ip_allocator.iter_allocated(self.storage) {
                    if !first {
                        write!(f, ", ")?;
                    }
                    write!(f, "{}", ip)?;
                    first = false;
                }
                write!(f, "]")?;
            }
        }

        fmt::Result::Ok(())
    }
}
