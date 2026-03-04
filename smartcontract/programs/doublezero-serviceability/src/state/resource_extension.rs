use crate::{
    error::DoubleZeroError, id_allocator::IdAllocator, ip_allocator::IpAllocator, resource::IdOrIp,
    state::accounttype::AccountType,
};
use doublezero_program_common::types::NetworkV4;

use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{account_info::AccountInfo, pubkey::Pubkey};
use std::{fmt, io::Cursor};

const _RESOURCE_EXTENSION_HEADER_SIZE_ID_ALLOCATOR: usize = 83;
const RESOURCE_EXTENSION_HEADER_SIZE_IP_ALLOCATOR: usize = 84;
const RESOURCE_EXTENSION_HEADER_SIZE: usize = RESOURCE_EXTENSION_HEADER_SIZE_IP_ALLOCATOR;
const RESOURCE_EXTENSION_BITMAP_OFFSET: usize = RESOURCE_EXTENSION_HEADER_SIZE.div_ceil(8) * 8; // Align to 8 bytes

#[repr(u8)]
#[derive(BorshSerialize, BorshDeserialize, Clone, Debug, PartialEq)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
#[borsh(use_discriminant = true)]
pub enum Allocator {
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
    pub account_type: AccountType, // 1
    pub owner: Pubkey,             // 32
    pub bump_seed: u8,             // 1
    pub associated_with: Pubkey,   // 32
    pub allocator: Allocator,      // Variable
    pub storage: Vec<u8>,          // Variable
}

impl ResourceExtensionOwned {
    pub fn iter_allocated(&self) -> Vec<IdOrIp> {
        match &self.allocator {
            Allocator::Ip(ip_allocator) => ip_allocator
                .iter_allocated(&self.storage)
                .map(|ip| NetworkV4::new(ip, 32).unwrap())
                .map(IdOrIp::Ip)
                .collect(),
            Allocator::Id(id_allocator) => id_allocator
                .iter_allocated(&self.storage)
                .map(IdOrIp::Id)
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
            associated_with: BorshDeserialize::deserialize(&mut cursor).unwrap_or_default(),
            allocator: BorshDeserialize::deserialize(&mut cursor).unwrap(),
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
            "ResourceExtensionOwned {{ account_type: {:?}, owner: {}, bump_seed: {}, associated_with: {}, allocator: {:?} }}",
            self.account_type,
            self.owner,
            self.bump_seed,
            self.associated_with,
            self.allocator,
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

#[derive(Debug, PartialEq)]
pub struct ResourceExtensionBorrowed<'a> {
    pub account_type: AccountType, // 1
    pub owner: Pubkey,             // 32
    pub bump_seed: u8,             // 1
    pub associated_with: Pubkey,   // 32
    pub allocator: Allocator,      // Variable
    pub storage: &'a mut [u8],
}

impl<'a> ResourceExtensionBorrowed<'a> {
    pub fn size(range: &ResourceExtensionRange) -> usize {
        match range {
            ResourceExtensionRange::IpBlock(base_net, _allocation_size) => {
                RESOURCE_EXTENSION_BITMAP_OFFSET
                    + IpAllocator::bitmap_required_size(base_net.prefix())
            }
            ResourceExtensionRange::IdRange(start, end) => {
                RESOURCE_EXTENSION_BITMAP_OFFSET + IdAllocator::bitmap_required_size((*start, *end))
            }
        }
    }

    pub fn construct_resource(
        account: &AccountInfo,
        owner: &Pubkey,
        bump_seed: u8,
        associated_with: &Pubkey,
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
        associated_with
            .serialize(&mut cursor)
            .map_err(|_| DoubleZeroError::SerializationFailure)?;
        match range {
            ResourceExtensionRange::IpBlock(base_net, _allocation_size) => {
                Allocator::Ip(IpAllocator::new(*base_net))
            }
            ResourceExtensionRange::IdRange(start, end) => Allocator::Id(
                IdAllocator::new((*start, *end))
                    .map_err(|_| DoubleZeroError::SerializationFailure)?,
            ),
        }
        .serialize(&mut cursor)
        .map_err(|_| DoubleZeroError::SerializationFailure)?;

        assert!(
            cursor.position() as usize <= RESOURCE_EXTENSION_BITMAP_OFFSET,
            "Cursor advanced more than RESOURCE_EXTENSION_BITMAP_OFFSET bytes"
        );

        Ok(())
    }

    pub fn inplace_from(data: &'a mut [u8]) -> Result<Self, DoubleZeroError> {
        let (data, bitmap) = data.split_at_mut(RESOURCE_EXTENSION_BITMAP_OFFSET);
        let mut cursor: &[u8] = data;
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut cursor).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut cursor).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut cursor).unwrap_or_default(),
            associated_with: BorshDeserialize::deserialize(&mut cursor).unwrap_or_default(),
            allocator: BorshDeserialize::deserialize(&mut cursor).unwrap(),
            storage: bitmap,
        };

        if out.account_type != AccountType::ResourceExtension {
            return Err(DoubleZeroError::InvalidAccountType);
        }

        Ok(out)
    }

    pub fn allocate(&mut self, count: usize) -> Result<IdOrIp, DoubleZeroError> {
        match &mut self.allocator {
            Allocator::Ip(ip_allocator) => {
                assert!(
                    count.is_power_of_two(),
                    "IP allocation count must be a power of two"
                );
                Ok(IdOrIp::Ip(
                    ip_allocator
                        .allocate(self.storage, count)
                        .ok_or(DoubleZeroError::AllocationFailed)?,
                ))
            }
            Allocator::Id(id_allocator) => {
                assert_eq!(count, 1, "ID allocation count must be 1");
                Ok(IdOrIp::Id(
                    id_allocator
                        .allocate(self.storage)
                        .ok_or(DoubleZeroError::AllocationFailed)?,
                ))
            }
        }
    }

    pub fn allocate_specific(&mut self, value: &IdOrIp) -> Result<(), DoubleZeroError> {
        match &mut self.allocator {
            Allocator::Ip(ip_allocator) => {
                let IdOrIp::Ip(ip) = value else {
                    return Err(DoubleZeroError::InvalidArgument);
                };
                ip_allocator
                    .allocate_specific(self.storage, ip)
                    .map_err(|_| DoubleZeroError::AllocationFailed)?;
            }
            Allocator::Id(id_allocator) => {
                let IdOrIp::Id(id) = value else {
                    return Err(DoubleZeroError::InvalidArgument);
                };
                id_allocator
                    .allocate_specific(self.storage, *id)
                    .map_err(|_| DoubleZeroError::AllocationFailed)?;
            }
        }
        Ok(())
    }

    pub fn deallocate(&mut self, value: &IdOrIp) -> bool {
        match &mut self.allocator {
            Allocator::Ip(ip_allocator) => {
                let IdOrIp::Ip(ip) = value else {
                    return false;
                };
                ip_allocator.deallocate(self.storage, ip)
            }
            Allocator::Id(id_allocator) => {
                let IdOrIp::Id(id) = value else {
                    return false;
                };
                id_allocator.deallocate(self.storage, *id)
            }
        }
    }

    pub fn count_allocated(&self) -> usize {
        match &self.allocator {
            Allocator::Ip(ip_allocator) => ip_allocator.iter_allocated(self.storage).count(),
            Allocator::Id(id_allocator) => id_allocator.iter_allocated(self.storage).count(),
        }
    }
}

impl fmt::Display for ResourceExtensionBorrowed<'_> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "ResourceExtensionBorrowed {{ account_type: {:?}, owner: {}, bump_seed: {}, associated_with: {}, allocator: {:?} }}",
            self.account_type,
            self.owner,
            self.bump_seed,
            self.associated_with,
            self.allocator,
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use borsh::object_length;

    fn construct_resource_extension(buffer: &mut [u8]) -> (Pubkey, Pubkey) {
        let account_pk = Pubkey::new_unique();
        let owner_pk = Pubkey::new_unique();
        ResourceExtensionBorrowed::construct_resource(
            &AccountInfo::new(
                &account_pk,
                false,
                true,
                &mut 0,
                buffer,
                &owner_pk,
                false,
                0,
            ),
            &owner_pk,
            1,
            &Pubkey::default(),
            &ResourceExtensionRange::IdRange(0, 64),
        )
        .unwrap();
        (account_pk, owner_pk)
    }

    #[test]
    fn test_resource_extension_header_size_id_allocator() {
        let empty_vec: Vec<u8> = vec![];
        let res_ext = ResourceExtensionOwned {
            account_type: AccountType::ResourceExtension,
            owner: Pubkey::default(),
            bump_seed: 0,
            associated_with: Pubkey::default(),
            allocator: Allocator::Id(IdAllocator::new((0, 10)).unwrap()),
            storage: empty_vec.clone(),
        };
        assert_eq!(
            object_length(&res_ext).unwrap(),
            _RESOURCE_EXTENSION_HEADER_SIZE_ID_ALLOCATOR + empty_vec.len()
        );
    }

    #[test]
    fn test_resource_extension_header_size_ip_allocator() {
        let empty_vec: Vec<u8> = vec![];
        let res_ext = ResourceExtensionOwned {
            account_type: AccountType::ResourceExtension,
            owner: Pubkey::default(),
            bump_seed: 0,
            associated_with: Pubkey::default(),
            allocator: Allocator::Ip(IpAllocator::new("1.1.1.1/24".parse().unwrap())),
            storage: empty_vec.clone(),
        };
        assert_eq!(
            object_length(&res_ext).unwrap(),
            RESOURCE_EXTENSION_HEADER_SIZE_IP_ALLOCATOR + empty_vec.len()
        );
    }

    #[test]
    fn test_resource_extension_owned_try_from_invalid() {
        let data = [0u8; RESOURCE_EXTENSION_BITMAP_OFFSET + 8];
        let result = ResourceExtensionOwned::try_from(&data[..]);
        assert!(result == Err(DoubleZeroError::InvalidAccountType));
    }

    #[test]
    fn test_resource_extension_owned_try_from_success() {
        let mut buffer =
            vec![0u8; ResourceExtensionBorrowed::size(&ResourceExtensionRange::IdRange(0, 64))];
        let (_, owner_pk) = construct_resource_extension(&mut buffer[..]);
        let resext = ResourceExtensionOwned::try_from(&buffer[..]).unwrap();
        assert_eq!(resext.account_type, AccountType::ResourceExtension);
        assert_eq!(resext.owner, owner_pk);
        assert_eq!(resext.bump_seed, 1);
        assert_eq!(resext.associated_with, Pubkey::default());
        match resext.allocator {
            Allocator::Id(id_allocator) => {
                assert_eq!(id_allocator.range, (0, 64));
            }
            _ => panic!("Expected IdAllocator"),
        }
        assert_eq!(
            resext.storage.len(),
            IdAllocator::bitmap_required_size((0, 64))
        );
    }

    #[test]
    fn test_resource_extension_borrowed_inplace_from_invalid() {
        let mut data = [0u8; RESOURCE_EXTENSION_BITMAP_OFFSET + 8];
        let result = ResourceExtensionBorrowed::inplace_from(&mut data[..]);
        assert!(result.is_ok() || result == Err(DoubleZeroError::InvalidAccountType));
    }

    #[test]
    fn test_resource_extension_borrowed_inplace_from_success() {
        let mut buffer =
            vec![0u8; ResourceExtensionBorrowed::size(&ResourceExtensionRange::IdRange(0, 64))];
        let (_, owner_pk) = construct_resource_extension(&mut buffer[..]);
        let resext = ResourceExtensionBorrowed::inplace_from(&mut buffer[..]).unwrap();
        assert_eq!(resext.account_type, AccountType::ResourceExtension);
        assert_eq!(resext.owner, owner_pk);
        assert_eq!(resext.bump_seed, 1);
        assert_eq!(resext.associated_with, Pubkey::default());
        match resext.allocator {
            Allocator::Id(id_allocator) => {
                assert_eq!(id_allocator.range, (0, 64));
            }
            _ => panic!("Expected IdAllocator"),
        }
        assert_eq!(
            resext.storage.len(),
            IdAllocator::bitmap_required_size((0, 64))
        );
    }

    #[test]
    fn test_allocate_and_deallocate_id_and_iter_allocated() {
        let mut buffer =
            vec![0u8; ResourceExtensionBorrowed::size(&ResourceExtensionRange::IdRange(0, 64))];
        let (_, _) = construct_resource_extension(&mut buffer[..]);
        let mut resext = ResourceExtensionBorrowed::inplace_from(&mut buffer[..]).unwrap();
        resext.allocate(1).unwrap();
        resext.allocate(1).unwrap();
        resext.allocate_specific(&IdOrIp::Id(5)).unwrap();
        resext.deallocate(&IdOrIp::Id(0));
        let resext = ResourceExtensionOwned::try_from(&buffer[..]).unwrap();
        let allocated = resext.iter_allocated();
        assert_eq!(allocated, vec![IdOrIp::Id(1), IdOrIp::Id(5)]);
    }

    #[test]
    fn test_resource_extension_owned_display_trait() {
        let owner_pk = Pubkey::default();
        let ext = ResourceExtensionOwned {
            account_type: AccountType::ResourceExtension,
            owner: owner_pk,
            bump_seed: 1,
            associated_with: Pubkey::default(),
            allocator: Allocator::Id(IdAllocator::new((0, 10)).unwrap()),
            storage: vec![],
        };
        let s = format!("{}", ext);
        assert_eq!(s, "ResourceExtensionOwned { account_type: ResourceExtension, owner: 11111111111111111111111111111111, bump_seed: 1, associated_with: 11111111111111111111111111111111, allocator: Id(IdAllocator { range: (0, 10), first_free_index: 0 }) }, allocated: []");
    }

    #[test]
    fn test_resource_extension_borrowed_display_trait() {
        let mut buffer =
            vec![0u8; ResourceExtensionBorrowed::size(&ResourceExtensionRange::IdRange(0, 10))];
        let owner_pk = Pubkey::default();
        let ext = ResourceExtensionBorrowed {
            account_type: AccountType::ResourceExtension,
            owner: owner_pk,
            bump_seed: 1,
            associated_with: Pubkey::default(),
            allocator: Allocator::Id(IdAllocator::new((0, 10)).unwrap()),
            storage: buffer.as_mut_slice(),
        };
        let s = format!("{}", ext);
        assert_eq!(s, "ResourceExtensionBorrowed { account_type: ResourceExtension, owner: 11111111111111111111111111111111, bump_seed: 1, associated_with: 11111111111111111111111111111111, allocator: Id(IdAllocator { range: (0, 10), first_free_index: 0 }) }");
    }
}
