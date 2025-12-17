use crate::{
    error::DoubleZeroError, id_allocator::IdAllocator, ip_allocator::IpAllocator, resource::IdOrIp,
    state::accounttype::AccountType,
};
use doublezero_program_common::types::NetworkV4;

use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, pubkey::Pubkey};
use std::{fmt, io::Cursor};

const RESOURCE_EXTENSION_HEADER_SIZE: usize = 76;
const RESOURCE_EXTENSION_PADDING: usize = 4;
const RESOURCE_EXTENSION_BITMAP_OFFSET: usize =
    RESOURCE_EXTENSION_HEADER_SIZE + RESOURCE_EXTENSION_PADDING;

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Clone, Debug, PartialEq)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
#[borsh(use_discriminant = true)]
pub enum ResourceExtensionType {
    Ip(IpAllocator),
    Id(IdAllocator),
}

pub enum ResourceExtensionRange {
    IpBlock(NetworkV4, u32),
    IdRange(u16, u16),
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
    pub fn iter_allocated(&self) -> Vec<IdOrIp> {
        match &self.extension_type {
            ResourceExtensionType::Ip(ip_allocator) => ip_allocator
                .iter_allocated(&self.storage)
                .map(|x| IdOrIp::Ip(x))
                .collect(),
            ResourceExtensionType::Id(id_allocator) => id_allocator
                .iter_allocated(&self.storage)
                .map(|x| IdOrIp::Id(x))
                .collect(),
        }
    }
}

impl TryFrom<&[u8]> for ResourceExtensionOwned {
    type Error = DoubleZeroError;

    fn try_from(data: &[u8]) -> Result<Self, Self::Error> {
        let (mut cursor, bitmap) = data.split_at(RESOURCE_EXTENSION_BITMAP_OFFSET);
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut cursor).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut cursor).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut cursor).unwrap_or_default(),
            assocatiated_with: BorshDeserialize::deserialize(&mut cursor).unwrap_or_default(),
            extension_type: BorshDeserialize::deserialize(&mut cursor).unwrap(),
            storage: bitmap.to_vec(),
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

        write!(f, ", allocated: [")?;
        let mut first = true;
        for val in self.iter_allocated() {
            if !first {
                write!(f, ", ")?;
            }
            write!(f, "{}", val)?;
            first = false;
        }
        write!(f, "]")?;

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
    pub fn size(range: &ResourceExtensionRange) -> usize {
        match range {
            ResourceExtensionRange::IpBlock(base_net, allocation_size) => {
                RESOURCE_EXTENSION_BITMAP_OFFSET as usize
                    + IpAllocator::bitmap_required_size(base_net.prefix(), *allocation_size)
            }
            ResourceExtensionRange::IdRange(start, end) => {
                RESOURCE_EXTENSION_BITMAP_OFFSET as usize
                    + IdAllocator::bitmap_required_size((*start, *end))
            }
        }
    }

    pub fn construct_resource(
        account: &AccountInfo,
        owner: &Pubkey,
        bump_seed: u8,
        assocatiated_with: &Pubkey,
        range: &ResourceExtensionRange,
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
        match range {
            ResourceExtensionRange::IpBlock(base_net, allocation_size) => {
                ResourceExtensionType::Ip(
                    IpAllocator::new(*base_net, *allocation_size)
                        .map_err(|_| DoubleZeroError::SerializationFailure)?,
                )
            }
            ResourceExtensionRange::IdRange(start, end) => ResourceExtensionType::Id(
                IdAllocator::new((*start, *end))
                    .map_err(|_| DoubleZeroError::SerializationFailure)?,
            ),
        }
        .serialize(&mut cursor)
        .map_err(|_| DoubleZeroError::SerializationFailure)?;

        Ok(())
    }

    pub fn inplace_from(data: &'a mut [u8]) -> Result<Self, DoubleZeroError> {
        let (data, bitmap) = data.split_at_mut(RESOURCE_EXTENSION_BITMAP_OFFSET);
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

    pub fn allocate(&mut self) -> Result<IdOrIp, DoubleZeroError> {
        match &mut self.extension_type {
            ResourceExtensionType::Ip(ip_allocator) => Ok(IdOrIp::Ip(
                ip_allocator
                    .allocate(self.storage)
                    .ok_or(DoubleZeroError::AllocationFailed)?,
            )),
            ResourceExtensionType::Id(id_allocator) => Ok(IdOrIp::Id(
                id_allocator
                    .allocate(self.storage)
                    .ok_or(DoubleZeroError::AllocationFailed)?,
            )),
        }
    }

    pub fn allocate_specific(&mut self, value: &IdOrIp) -> Result<(), DoubleZeroError> {
        match &mut self.extension_type {
            ResourceExtensionType::Ip(ip_allocator) => {
                let &IdOrIp::Ip(ref ip) = value else {
                    return Err(DoubleZeroError::InvalidArgument);
                };
                ip_allocator
                    .allocate_specific(self.storage, &ip)
                    .map_err(|_| DoubleZeroError::AllocationFailed)?;
                Ok(())
            }
            ResourceExtensionType::Id(id_allocator) => {
                let &IdOrIp::Id(id) = value else {
                    return Err(DoubleZeroError::InvalidArgument);
                };
                id_allocator
                    .allocate_specific(self.storage, id)
                    .map_err(|_| DoubleZeroError::AllocationFailed)?;
                Ok(())
            }
        }
    }

    pub fn deallocate(&mut self, value: &IdOrIp) -> bool {
        match &mut self.extension_type {
            ResourceExtensionType::Ip(ip_allocator) => {
                let &IdOrIp::Ip(ref ip) = value else {
                    return false;
                };
                ip_allocator.deallocate(self.storage, &ip)
            }
            ResourceExtensionType::Id(id_allocator) => {
                let &IdOrIp::Id(id) = value else {
                    return false;
                };
                id_allocator.deallocate(self.storage, id)
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
        )
    }
}
