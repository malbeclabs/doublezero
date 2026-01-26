use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::types::NetworkV4;
use std::net::Ipv4Addr;

/// Manages a block of allocatable IP addresses using a bitmap.
/// Each bit represents a single /32 address.
#[derive(BorshSerialize, BorshDeserialize, Clone, Debug, PartialEq)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct IpAllocator {
    /// The starting IP address and netmask of the resource block.
    pub base_net: NetworkV4,
    pub first_free_index: usize,
}

impl IpAllocator {
    pub fn bitmap_required_size(prefix_len: u8) -> usize {
        let total_addresses = 2_usize.pow(32 - prefix_len as u32);
        total_addresses.div_ceil(64) * 8 // must be a multiple of 64bits
    }

    pub fn check_bitmap_require_size(bitmap: &[u8], prefix_len: u8) -> bool {
        let required_size = Self::bitmap_required_size(prefix_len);
        bitmap.len() >= required_size
    }

    pub fn new(base_net: NetworkV4) -> Self {
        IpAllocator {
            base_net,
            first_free_index: 0,
        }
    }

    /// Allocate a network with the specified prefix length.
    /// allocation_size must be <= 64.
    /// Returns None if no contiguous block of the required size is available.
    pub fn allocate(&mut self, bitmap: &mut [u8], allocation_size: usize) -> Option<NetworkV4> {
        let prefix_len = 32 - (allocation_size as u32).trailing_zeros() as u8;
        if prefix_len < self.base_net.prefix() || prefix_len < 26 {
            return None;
        }

        let total_addresses = self.base_net.size() as usize;
        let base_ip_int = u32::from_be_bytes(self.base_net.ip().octets());

        // Reinterpret bitmap as u64 slice for faster scanning
        let bitmap_u64: &mut [u64] = bytemuck::cast_slice_mut(bitmap);

        for (word_index, word) in bitmap_u64
            .iter_mut()
            .enumerate()
            .skip(self.first_free_index)
        {
            // Quick check: if allocation fits in one word and word has free bits
            if allocation_size <= 64 && *word != u64::MAX {
                let allocs_per_word = 64 / allocation_size;
                let mask = (1u64 << allocation_size) - 1;

                for slot in 0..allocs_per_word {
                    let bit_offset = slot * allocation_size;
                    let bit_index = word_index * 64 + bit_offset;

                    if bit_index + allocation_size > total_addresses {
                        return None;
                    }

                    if (*word >> bit_offset) & mask == 0 {
                        // Found free slot, mark as allocated
                        *word |= mask << bit_offset;

                        let allocated_ip_int = base_ip_int + bit_index as u32;
                        let allocated_ip = Ipv4Addr::from(allocated_ip_int);
                        let allocated_net = NetworkV4::new(allocated_ip, prefix_len)
                            .expect("Valid IP and prefix length");

                        if (word_index + 1) * 64 >= total_addresses {
                            self.first_free_index = 0;
                        } else {
                            self.first_free_index = word_index;
                        }
                        return Some(allocated_net);
                    }
                }
            }
        }

        None
    }

    /// Allocate a specific network.
    pub fn allocate_specific(
        &mut self,
        bitmap: &mut [u8],
        ip_net: &NetworkV4,
    ) -> Result<(), String> {
        if !ip_net.is_subnet_of(&self.base_net) {
            return Err("The specified IP is outside the base network.".into());
        }

        if ip_net.prefix() < self.base_net.prefix() {
            return Err("The specified prefix is larger than the base network.".into());
        }

        let base_ip_int = u32::from_be_bytes(self.base_net.ip().octets());
        let ip_int = u32::from_be_bytes(ip_net.ip().octets());
        let allocation_size = prefix_len_to_address_count(ip_net.prefix());

        let offset = ip_int.checked_sub(base_ip_int).unwrap() as usize;
        if offset % allocation_size != 0 {
            return Err("Requested IP is not aligned to allocation size.".into());
        }

        if offset + allocation_size > self.base_net.size() as usize {
            return Err("The specified IP is outside the allocatable range.".into());
        }

        if !self.is_range_free(bitmap, offset, allocation_size) {
            return Err(
                "The specified IP range is already allocated (or partially allocated).".into(),
            );
        }

        self.set_range(bitmap, offset, allocation_size, true);

        Ok(())
    }

    pub fn deallocate(&mut self, bitmap: &mut [u8], release_net: &NetworkV4) -> bool {
        if release_net.prefix() < self.base_net.prefix()
            || !release_net.is_subnet_of(&self.base_net)
        {
            return false;
        }

        let base_ip_int = u32::from_be_bytes(self.base_net.ip().octets());
        let release_ip_int = u32::from_be_bytes(release_net.ip().octets());
        let allocation_size = prefix_len_to_address_count(release_net.prefix());

        let offset = (release_ip_int - base_ip_int) as usize;

        if offset + allocation_size > bitmap.len() * 8 {
            return false;
        }

        // Check if all bits in the range are set
        if !self.is_range_allocated(bitmap, offset, allocation_size) {
            return false;
        }

        self.set_range(bitmap, offset, allocation_size, false);

        // Update first_free_index hint
        let u64_index = offset / 64;
        if u64_index < self.first_free_index {
            self.first_free_index = u64_index;
        }

        true
    }

    /// Check if a range of bits is entirely free (all zeros)
    fn is_range_free(&self, bitmap: &[u8], start_bit: usize, count: usize) -> bool {
        for i in 0..count {
            let bit_index = start_bit + i;
            let byte_index = bit_index / 8;
            let bit_offset = bit_index % 8;
            if (bitmap[byte_index] & (1 << bit_offset)) != 0 {
                return false;
            }
        }
        true
    }

    /// Check if a range of bits is entirely allocated (all ones)
    fn is_range_allocated(&self, bitmap: &[u8], start_bit: usize, count: usize) -> bool {
        for i in 0..count {
            let bit_index = start_bit + i;
            let byte_index = bit_index / 8;
            let bit_offset = bit_index % 8;
            if (bitmap[byte_index] & (1 << bit_offset)) == 0 {
                return false;
            }
        }
        true
    }

    /// Set or clear a range of bits
    fn set_range(&self, bitmap: &mut [u8], start_bit: usize, count: usize, value: bool) {
        for i in 0..count {
            let bit_index = start_bit + i;
            let byte_index = bit_index / 8;
            let bit_offset = bit_index % 8;
            if value {
                bitmap[byte_index] |= 1 << bit_offset;
            } else {
                bitmap[byte_index] &= !(1 << bit_offset);
            }
        }
    }

    pub fn iter_allocated<'a>(&'a self, bitmap: &'a [u8]) -> impl Iterator<Item = Ipv4Addr> + 'a {
        let base_addr = self.base_net.ip().to_bits();
        bitmap.iter().enumerate().flat_map(move |(byte_idx, byte)| {
            (0..8).filter_map(move |bit_idx| {
                let i = byte_idx * 8 + bit_idx;
                if (byte >> bit_idx) & 1 == 1 {
                    Some(Ipv4Addr::from_bits(base_addr + i as u32))
                } else {
                    None
                }
            })
        })
    }

    pub fn try_from(mut data: &[u8]) -> Result<Self, String> {
        let base_net = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let first_free_index = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        Ok(Self {
            base_net,
            first_free_index,
        })
    }
}

fn prefix_len_to_address_count(prefix_len: u8) -> usize {
    1 << (32 - prefix_len)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[repr(align(8))]
    struct AlignedBitmap([u8; 8]);

    #[test]
    fn test_allocate_and_deallocate() {
        // 192.168.1.0/30 has 4 addresses, /32 allocations = 4 allocatable blocks
        let mut aligned_data = AlignedBitmap([0u8; 8]);
        let mut allocator = IpAllocator::new("192.168.1.0/30".parse().unwrap());

        let mut allocated = vec![];
        for _ in 0..4 {
            let net = allocator.allocate(&mut aligned_data.0, 1);
            assert!(net.is_some());
            allocated.push(net.unwrap());
        }
        // No more allocations should be possible
        assert!(allocator.allocate(&mut aligned_data.0, 1).is_none());

        // Deallocate one and allocate again
        assert!(allocator.deallocate(&mut aligned_data.0, &allocated[2]));
        let net = allocator.allocate(&mut aligned_data.0, 1);
        assert_eq!(net, Some(allocated[2]));
    }

    #[test]
    fn test_deallocate_invalid() {
        let mut aligned_data = AlignedBitmap([0u8; 8]);
        let mut allocator = IpAllocator::new("10.0.0.0/30".parse().unwrap());

        // Not allocated yet
        let net = "10.0.0.2/32".parse().unwrap();
        assert!(!allocator.deallocate(&mut aligned_data.0, &net));

        // Wrong prefix
        let net_wrong = "10.0.0.2/31".parse().unwrap();
        assert!(!allocator.deallocate(&mut aligned_data.0, &net_wrong));

        // Outside base net
        let net_out = "10.0.1.0/32".parse().unwrap();
        assert!(!allocator.deallocate(&mut aligned_data.0, &net_out));
    }

    #[test]
    fn test_allocate_specific_success() {
        let base_net = "192.168.0.0/24".parse().unwrap();
        let mut aligned_data = AlignedBitmap([0u8; 8]);
        let mut allocator = IpAllocator::new(base_net);

        let ip = "192.168.0.16/28".parse().unwrap();
        let result = allocator.allocate_specific(&mut aligned_data.0, &ip);
        assert!(result.is_ok());
        assert!(allocator.deallocate(&mut aligned_data.0, &ip));
    }

    #[test]
    fn test_allocate_specific_not_in_base_net() {
        let base_net = "192.168.0.0/24".parse().unwrap();
        let mut aligned_data = AlignedBitmap([0u8; 8]);
        let mut allocator = IpAllocator::new(base_net);
        let ip = "10.0.0.0/28".parse().unwrap();
        assert!(allocator
            .allocate_specific(&mut aligned_data.0, &ip)
            .is_err());
    }

    #[test]
    fn test_allocate_specific_not_aligned() {
        let base_net = "192.168.0.0/24".parse().unwrap();
        let mut aligned_data = AlignedBitmap([0u8; 8]);
        let mut allocator = IpAllocator::new(base_net);
        let ip = "192.168.0.3/28".parse().unwrap(); // Not aligned to allocation_size=16
        assert!(allocator
            .allocate_specific(&mut aligned_data.0, &ip)
            .is_err());
    }

    #[test]
    fn test_allocate_specific_already_allocated() {
        let base_net = "192.168.0.0/24".parse().unwrap();
        let mut aligned_data = AlignedBitmap([0u8; 8]);
        let mut allocator = IpAllocator::new(base_net);
        let ip = "192.168.0.32/28".parse().unwrap();
        assert!(allocator
            .allocate_specific(&mut aligned_data.0, &ip)
            .is_ok());
        // Try to allocate again
        assert!(allocator
            .allocate_specific(&mut aligned_data.0, &ip)
            .is_err());
    }

    #[test]
    fn test_iter_allocated() {
        let base_net = "192.168.0.0/24".parse().unwrap();
        let mut aligned_data = AlignedBitmap([0u8; 8]);
        let mut allocator = IpAllocator::new(base_net);
        assert!(allocator.allocate(&mut aligned_data.0, 1).is_some());
        assert!(allocator.allocate(&mut aligned_data.0, 1).is_some());
        assert!(allocator.allocate(&mut aligned_data.0, 1).is_some());
        assert!(allocator.allocate(&mut aligned_data.0, 1).is_some());

        let ip = "192.168.0.10/32".parse().unwrap();
        assert!(allocator
            .allocate_specific(&mut aligned_data.0, &ip)
            .is_ok());

        let ip = "192.168.0.42/32".parse().unwrap();
        assert!(allocator
            .allocate_specific(&mut aligned_data.0, &ip)
            .is_ok());

        let allocated_ips: Vec<Ipv4Addr> = allocator.iter_allocated(&aligned_data.0).collect();
        assert_eq!(allocated_ips.len(), 6);
        assert_eq!(allocated_ips[0], "192.168.0.0".parse::<Ipv4Addr>().unwrap());
        assert_eq!(allocated_ips[1], "192.168.0.1".parse::<Ipv4Addr>().unwrap());
        assert_eq!(allocated_ips[2], "192.168.0.2".parse::<Ipv4Addr>().unwrap());
        assert_eq!(allocated_ips[3], "192.168.0.3".parse::<Ipv4Addr>().unwrap());
        assert_eq!(
            allocated_ips[4],
            "192.168.0.10".parse::<Ipv4Addr>().unwrap()
        );
        assert_eq!(
            allocated_ips[5],
            "192.168.0.42".parse::<Ipv4Addr>().unwrap()
        );

        assert!(allocator.deallocate(&mut aligned_data.0, &"192.168.0.1/32".parse().unwrap()));
        assert!(allocator.deallocate(&mut aligned_data.0, &"192.168.0.3/32".parse().unwrap()));

        let allocated_ips: Vec<Ipv4Addr> = allocator.iter_allocated(&aligned_data.0).collect();
        assert_eq!(allocated_ips.len(), 4);
        assert_eq!(allocated_ips[0], "192.168.0.0".parse::<Ipv4Addr>().unwrap());
        assert_eq!(allocated_ips[1], "192.168.0.2".parse::<Ipv4Addr>().unwrap());
        assert_eq!(
            allocated_ips[2],
            "192.168.0.10".parse::<Ipv4Addr>().unwrap()
        );
        assert_eq!(
            allocated_ips[3],
            "192.168.0.42".parse::<Ipv4Addr>().unwrap()
        );
    }
}
