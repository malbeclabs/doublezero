use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::types::NetworkV4;
use std::net::Ipv4Addr;

/// Manages a block of allocatable IP addresses using a bitmap.
#[derive(BorshSerialize, BorshDeserialize, Clone, Debug, PartialEq)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct IpAllocator {
    /// The starting IP address and netmask of the resource block.
    pub base_net: NetworkV4,
    /// The number of **individual addresses** in one allocation block.
    /// E.g., 1 for /32, 2 for /31, 8 for /29.
    pub allocation_size: u32,
    pub first_free_index: usize,
}

impl IpAllocator {
    pub fn bitmap_required_size(prefix_len: u8, allocation_size: u32) -> usize {
        let total_allocations = 2_usize.pow(32 - prefix_len as u32) / allocation_size as usize;
        total_allocations.div_ceil(64) * 8 // must be a multiple of 64bits
    }

    pub fn check_bitmap_require_size(bitmap: &[u8], prefix_len: u8, allocation_size: u32) -> bool {
        let required_size = Self::bitmap_required_size(prefix_len, allocation_size);
        bitmap.len() >= required_size
    }

    pub fn new(base_net: NetworkV4, allocation_size: u32) -> Result<Self, String> {
        let allocation_prefix_len =
            address_count_to_prefix_len(allocation_size).ok_or("Invalid allocation size.")?;

        if allocation_prefix_len < base_net.prefix() || allocation_prefix_len > 32 {
            return Err("Allocation prefix must be within the base net's range (base_net.prefix_len() <= allocation_prefix_len <= 32)".into());
        }

        // Total number of allocatable blocks in the base net
        let total_addresses: u32 = base_net.size();
        let total_allocations = total_addresses / allocation_size;

        if total_allocations == 0 {
            return Err("The base network is too small for the specified allocation size.".into());
        }

        Ok(IpAllocator {
            base_net,
            allocation_size,
            first_free_index: 0,
        })
    }

    pub fn allocate(&mut self, bitmap: &mut [u8]) -> Option<NetworkV4> {
        use bytemuck::cast_slice_mut;

        let allocation_prefix_len = address_count_to_prefix_len(self.allocation_size).unwrap();
        let total_blocks = (self.base_net.size() / self.allocation_size) as usize;
        let base_ip_int = u32::from_be_bytes(self.base_net.ip().octets());

        // Convert bitmap to &[u64] for efficient processing
        let u64_bitmap = cast_slice_mut::<u8, u64>(bitmap);

        for (u64_index, entry) in u64_bitmap
            .iter_mut()
            .enumerate()
            .skip(self.first_free_index)
        {
            if entry == &u64::MAX {
                continue; // All bits are allocated in this u64
            }
            for bit in 0..64 {
                let block_index = u64_index * 64 + bit;
                if block_index >= total_blocks {
                    break;
                }
                if (*entry & (1 << bit)) == 0 {
                    *entry |= 1 << bit;

                    let allocated_ip_int =
                        base_ip_int + (block_index as u32 * self.allocation_size);
                    let allocated_ip = Ipv4Addr::from(allocated_ip_int);

                    let allocated_net = NetworkV4::new(allocated_ip, allocation_prefix_len)
                        .expect("Valid IP and prefix length");

                    if entry != &u64::MAX {
                        self.first_free_index = u64_index;
                    } else {
                        // TODO: This could be optimized further by searching for the next free
                        // index
                        self.first_free_index = 0;
                    }

                    return Some(allocated_net);
                }
            }
        }

        None
    }
    pub fn allocate_specific(
        &mut self,
        bitmap: &mut [u8],
        ip_net: &NetworkV4,
    ) -> Result<(), String> {
        // Check if the ip_net is within the base network
        if !ip_net.is_subnet_of(&self.base_net) {
            return Err("The specified IP is outside the base network.".into());
        }

        if ip_net.prefix() != address_count_to_prefix_len(self.allocation_size).unwrap() {
            return Err("The specified IP does not match the allocation size.".into());
        }

        let base_ip_int = u32::from_be_bytes(self.base_net.ip().octets());
        let ip_int = u32::from_be_bytes(ip_net.ip().octets());

        // Calculate offset and check alignment
        let offset = ip_int.checked_sub(base_ip_int).unwrap();
        if offset % self.allocation_size != 0 {
            return Err("Requested IP is not aligned to allocation size.".into());
        }

        let global_bit_index = offset / self.allocation_size;
        let total_bits = self.base_net.size() / self.allocation_size;
        if global_bit_index >= total_bits {
            return Err("The specified IP is outside the allocatable range.".into());
        }

        let byte_index = (global_bit_index / 8) as usize;
        let bit_offset = (global_bit_index % 8) as u8;
        let mask: u8 = 1 << bit_offset;

        if (bitmap[byte_index] & mask) != 0 {
            return Err("The specified IP is already allocated.".into());
        }

        bitmap[byte_index] |= mask;

        if bitmap[byte_index] == 0xFF {
            // TODO optimize to find next free index
            self.first_free_index = 0;
        }

        Ok(())
    }

    pub fn deallocate(&mut self, bitmap: &mut [u8], release_net: &NetworkV4) -> bool {
        let allocation_prefix_len = address_count_to_prefix_len(self.allocation_size).unwrap();

        // 1. Validate the network being released
        if release_net.prefix() != allocation_prefix_len
            || !release_net.is_subnet_of(&self.base_net)
        {
            return false;
        }

        // 2. Calculate the global bit index
        let base_ip_v4 = self.base_net.ip();
        let release_ip_v4 = release_net.ip();

        let base_ip_int = u32::from_be_bytes(base_ip_v4.octets());
        let release_ip_int = u32::from_be_bytes(release_ip_v4.octets());

        let offset_addresses = release_ip_int - base_ip_int;

        // The index of the bit to clear
        let global_bit_index = offset_addresses / self.allocation_size;

        let total_bits = (bitmap.len() * 8) as u32;

        if global_bit_index >= total_bits {
            return false;
        }

        // 3. Clear the corresponding bit
        let byte_index = (global_bit_index / 8) as usize;
        let bit_offset = (global_bit_index % 8) as u8;
        let mask: u8 = 1 << bit_offset;

        // Check if the bit was set before clearing (optional but good for debugging/validation)
        let was_set = (bitmap[byte_index] & mask) != 0;

        // Clear the bit (set to 0)
        bitmap[byte_index] &= !mask;

        if was_set {
            self.first_free_index = byte_index / 8;
        }

        was_set
    }

    pub fn iter_allocated<'a>(&'a self, bitmap: &'a [u8]) -> impl Iterator<Item = NetworkV4> + 'a {
        let base_addr = self.base_net.ip().to_bits();
        let netmask = address_count_to_prefix_len(self.allocation_size).unwrap();
        bitmap.iter().enumerate().flat_map(move |(byte_idx, byte)| {
            (0..8).filter_map(move |bit_idx| {
                let i = byte_idx * 8 + bit_idx;
                if (byte >> bit_idx) & 1 == 1 {
                    // Calculate the starting address for this allocation
                    let addr = base_addr + (i as u32) * self.allocation_size;
                    Some(NetworkV4::new(Ipv4Addr::from_bits(addr), netmask).unwrap())
                } else {
                    None
                }
            })
        })
    }

    pub fn try_from(mut data: &[u8]) -> Result<Self, String> {
        let base_net = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let allocation_size = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let first_free_index = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        Ok(Self {
            base_net,
            allocation_size,
            first_free_index,
        })
    }
}

fn address_count_to_prefix_len(num_addresses: u32) -> Option<u8> {
    if num_addresses == 0 || (num_addresses & (num_addresses - 1)) != 0 {
        return None; // Cannot convert non-power-of-2 address count
    }

    let host_bits = num_addresses.trailing_zeros();
    Some(32 - host_bits as u8)
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
        let mut allocator = IpAllocator::new("192.168.1.0/30".parse().unwrap(), 1).unwrap();

        let mut allocated = vec![];
        for _ in 0..4 {
            let net = allocator.allocate(&mut aligned_data.0);
            assert!(net.is_some());
            allocated.push(net.unwrap());
        }
        // No more allocations should be possible
        assert!(allocator.allocate(&mut aligned_data.0).is_none());

        // Deallocate one and allocate again
        assert!(allocator.deallocate(&mut aligned_data.0, &allocated[2]));
        let net = allocator.allocate(&mut aligned_data.0);
        assert_eq!(net, Some(allocated[2]));
    }

    #[test]
    fn test_deallocate_invalid() {
        let mut aligned_data = AlignedBitmap([0u8; 8]);
        let mut allocator = IpAllocator::new("10.0.0.0/30".parse().unwrap(), 1).unwrap();

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
    fn test_bitmap_length_mismatch() {
        let base_net = "192.168.0.0/29".parse().unwrap();
        // /32 allocations: 8 addresses, 8 allocatable blocks, needs 1 byte
        let res = IpAllocator::new(base_net, 32);
        assert!(res.is_err());
    }

    #[test]
    fn test_allocation_prefix_range_check() {
        let base_net = "192.168.0.0/24".parse().unwrap();
        // allocation_prefix_len < base_net.prefix():
        // base_net is /24 (prefix=24), allocation_size=512 gives prefix=23
        // 23 < 24, so this should fail (can't allocate blocks larger than the base net)
        let res = IpAllocator::new(base_net, 512);
        assert!(res.is_err());
    }

    #[test]
    fn test_allocate_specific_success() {
        let base_net = "192.168.0.0/24".parse().unwrap();
        let mut aligned_data = AlignedBitmap([0u8; 8]);
        let mut allocator = IpAllocator::new(base_net, 16).unwrap();

        let ip = "192.168.0.16/28".parse().unwrap();
        let result = allocator.allocate_specific(&mut aligned_data.0, &ip);
        assert!(result.is_ok());
        assert!(allocator.deallocate(&mut aligned_data.0, &ip));
    }

    #[test]
    fn test_allocate_specific_mismatched_allocation_size() {
        let base_net = "192.168.0.0/24".parse().unwrap();
        let mut aligned_data = AlignedBitmap([0u8; 8]);
        let mut allocator = IpAllocator::new(base_net, 16).unwrap();

        let ip = "192.168.0.16/32".parse().unwrap();
        let result = allocator.allocate_specific(&mut aligned_data.0, &ip);
        assert!(result.is_err());
    }

    #[test]
    fn test_allocate_specific_not_in_base_net() {
        let base_net = "192.168.0.0/24".parse().unwrap();
        let mut aligned_data = AlignedBitmap([0u8; 8]);
        let mut allocator = IpAllocator::new(base_net, 16).unwrap();
        let ip = "10.0.0.0/28".parse().unwrap();
        assert!(allocator
            .allocate_specific(&mut aligned_data.0, &ip)
            .is_err());
    }

    #[test]
    fn test_allocate_specific_not_aligned() {
        let base_net = "192.168.0.0/24".parse().unwrap();
        let mut aligned_data = AlignedBitmap([0u8; 8]);
        let mut allocator = IpAllocator::new(base_net, 16).unwrap();
        let ip = "192.168.0.3/28".parse().unwrap(); // Not aligned to allocation_size=16
        assert!(allocator
            .allocate_specific(&mut aligned_data.0, &ip)
            .is_err());
    }

    #[test]
    fn test_allocate_specific_already_allocated() {
        let base_net = "192.168.0.0/24".parse().unwrap();
        let mut aligned_data = AlignedBitmap([0u8; 8]);
        let mut allocator = IpAllocator::new(base_net, 16).unwrap();
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
        let mut allocator = IpAllocator::new(base_net, 1).unwrap();
        assert!(allocator.allocate(&mut aligned_data.0).is_some());
        assert!(allocator.allocate(&mut aligned_data.0).is_some());
        assert!(allocator.allocate(&mut aligned_data.0).is_some());
        assert!(allocator.allocate(&mut aligned_data.0).is_some());

        let ip = "192.168.0.10/32".parse().unwrap();
        assert!(allocator
            .allocate_specific(&mut aligned_data.0, &ip)
            .is_ok());

        let ip = "192.168.0.42/32".parse().unwrap();
        assert!(allocator
            .allocate_specific(&mut aligned_data.0, &ip)
            .is_ok());

        let allocated_ips: Vec<NetworkV4> = allocator.iter_allocated(&aligned_data.0).collect();
        assert_eq!(allocated_ips.len(), 6);
        assert_eq!(allocated_ips[0], "192.168.0.0/32".parse().unwrap());
        assert_eq!(allocated_ips[1], "192.168.0.1/32".parse().unwrap());
        assert_eq!(allocated_ips[2], "192.168.0.2/32".parse().unwrap());
        assert_eq!(allocated_ips[3], "192.168.0.3/32".parse().unwrap());
        assert_eq!(allocated_ips[4], "192.168.0.10/32".parse().unwrap());
        assert_eq!(allocated_ips[5], "192.168.0.42/32".parse().unwrap());

        assert!(allocator.deallocate(&mut aligned_data.0, &"192.168.0.1/32".parse().unwrap()));
        assert!(allocator.deallocate(&mut aligned_data.0, &"192.168.0.3/32".parse().unwrap()));

        let allocated_ips: Vec<NetworkV4> = allocator.iter_allocated(&aligned_data.0).collect();
        assert_eq!(allocated_ips.len(), 4);
        assert_eq!(allocated_ips[0], "192.168.0.0/32".parse().unwrap());
        assert_eq!(allocated_ips[1], "192.168.0.2/32".parse().unwrap());
        assert_eq!(allocated_ips[2], "192.168.0.10/32".parse().unwrap());
        assert_eq!(allocated_ips[3], "192.168.0.42/32".parse().unwrap());
    }
}
