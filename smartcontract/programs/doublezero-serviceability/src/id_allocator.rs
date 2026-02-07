use borsh::{BorshDeserialize, BorshSerialize};

/// Manages a block of allocatable IP addresses using a bitmap.
#[derive(BorshSerialize, BorshDeserialize, Clone, Debug, PartialEq)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct IdAllocator {
    /// The range to allocate from [x, y)
    pub range: (u16, u16),
    pub first_free_index: usize,
}

impl IdAllocator {
    pub fn bitmap_required_size(range: (u16, u16)) -> usize {
        // Calculate bits needed, then round up to nearest multiple of 8 bytes (64 bits)
        // to ensure compatibility with bytemuck::cast_slice_mut::<u8, u64>
        let bits_needed = (range.1 - range.0) as usize;
        let bytes_needed = bits_needed.div_ceil(8);
        // Round up to multiple of 8 bytes for u64 alignment
        bytes_needed.next_multiple_of(8)
    }

    pub fn new(range: (u16, u16)) -> Result<Self, String> {
        if range.0 >= range.1 {
            return Err("Invalid range: start must be less than end".to_string());
        }
        Ok(IdAllocator {
            range,
            first_free_index: 0,
        })
    }

    pub fn allocate(&mut self, bitmap: &mut [u8]) -> Option<u16> {
        use bytemuck::cast_slice_mut;

        let total_blocks = (self.range.1 - self.range.0) as usize;

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

                    let allocated_id = self.range.0 + (block_index as u16);

                    if entry != &u64::MAX {
                        self.first_free_index = u64_index;
                    } else {
                        // TODO optimize to find next free index
                        self.first_free_index = 0;
                    }

                    return Some(allocated_id);
                }
            }
        }

        None
    }

    pub fn allocate_specific(&mut self, bitmap: &mut [u8], id: u16) -> Result<(), String> {
        if id < self.range.0 || id >= self.range.1 {
            return Err("The specified ID is outside the allocatable range.".into());
        }

        // Calculate offset and check alignment
        let global_bit_index = id.checked_sub(self.range.0).unwrap();
        let byte_index = (global_bit_index / 8) as usize;
        let bit_offset = (global_bit_index % 8) as u8;
        let mask: u8 = 1 << bit_offset;

        if (bitmap[byte_index] & mask) != 0 {
            return Err("The specified ID is already allocated.".into());
        }

        bitmap[byte_index] |= mask;

        if bitmap[byte_index] == 0xFF {
            // TODO optimize to find next free index
            self.first_free_index = 0;
        }

        Ok(())
    }

    pub fn deallocate(&mut self, bitmap: &mut [u8], id: u16) -> bool {
        if id < self.range.0 || id >= self.range.1 {
            return false;
        }

        let global_bit_index = id - self.range.0;
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

    pub fn iter_allocated<'a>(&'a self, bitmap: &'a [u8]) -> impl Iterator<Item = u16> + 'a {
        bitmap.iter().enumerate().flat_map(move |(byte_idx, byte)| {
            (0..8).filter_map(move |bit_idx| {
                let i = byte_idx * 8 + bit_idx;
                if (byte >> bit_idx) & 1 == 1 {
                    Some(self.range.0 + (i as u16))
                } else {
                    None
                }
            })
        })
    }

    pub fn try_from(mut data: &[u8]) -> Result<Self, String> {
        let range: (u16, u16) = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        let first_free_index: usize = BorshDeserialize::deserialize(&mut data).unwrap_or_default();
        Ok(Self {
            range,
            first_free_index,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[repr(align(8))]
    struct AlignedBitmap([u8; 8]);

    #[test]
    fn test_allocate_and_deallocate() {
        let mut aligned_data = AlignedBitmap([0u8; 8]);
        let mut allocator = IdAllocator::new((500, 510)).unwrap();

        let mut allocated = vec![];
        for _ in 0..10 {
            let net = allocator.allocate(&mut aligned_data.0);
            assert!(net.is_some());
            allocated.push(net.unwrap());
        }
        // No more allocations should be possible
        assert!(allocator.allocate(&mut aligned_data.0).is_none());

        // Deallocate one and allocate again
        assert!(allocator.deallocate(&mut aligned_data.0, allocated[2]));
        let net = allocator.allocate(&mut aligned_data.0);
        assert_eq!(net, Some(allocated[2]));
    }

    #[test]
    fn test_deallocate_invalid() {
        let mut aligned_data = AlignedBitmap([0u8; 8]);
        let mut allocator = IdAllocator::new((500, 510)).unwrap();

        // Not allocated yet
        assert!(!allocator.deallocate(&mut aligned_data.0, 505));

        // Outside range
        assert!(!allocator.deallocate(&mut aligned_data.0, 400));
    }

    #[test]
    fn test_allocation_range_check() {
        let res = IdAllocator::new((510, 500));
        assert!(res.is_err());
    }

    #[test]
    fn test_bitmap_required_size_multiple_of_8() {
        // All returned sizes must be multiples of 8 for bytemuck::cast_slice_mut::<u8, u64>
        assert_eq!(IdAllocator::bitmap_required_size((0, 1)), 8);
        assert_eq!(IdAllocator::bitmap_required_size((0, 8)), 8);
        assert_eq!(IdAllocator::bitmap_required_size((0, 64)), 8);
        assert_eq!(IdAllocator::bitmap_required_size((0, 65)), 16);
        assert_eq!(IdAllocator::bitmap_required_size((0, 80)), 16);
        assert_eq!(IdAllocator::bitmap_required_size((0, 128)), 16);
        assert_eq!(IdAllocator::bitmap_required_size((500, 628)), 16);
    }

    #[test]
    fn test_allocate_specific_success() {
        let mut aligned_data = AlignedBitmap([0u8; 8]);
        let mut allocator = IdAllocator::new((500, 510)).unwrap();

        let result = allocator.allocate_specific(&mut aligned_data.0, 509);
        assert!(result.is_ok());
        assert!(allocator.deallocate(&mut aligned_data.0, 509));
    }

    #[test]
    fn test_allocate_specific_not_in_base_net() {
        let mut aligned_data = AlignedBitmap([0u8; 8]);
        let mut allocator = IdAllocator::new((500, 510)).unwrap();
        assert!(allocator
            .allocate_specific(&mut aligned_data.0, 515)
            .is_err());
    }

    #[test]
    fn test_allocate_specific_already_allocated() {
        let mut aligned_data = AlignedBitmap([0u8; 8]);
        let mut allocator = IdAllocator::new((500, 510)).unwrap();
        assert!(allocator
            .allocate_specific(&mut aligned_data.0, 505)
            .is_ok());
        // Try to allocate again
        assert!(allocator
            .allocate_specific(&mut aligned_data.0, 505)
            .is_err());
    }

    #[test]
    fn test_iter_allocated() {
        let mut aligned_data = AlignedBitmap([0u8; 8]);
        let mut allocator = IdAllocator::new((500, 600)).unwrap();
        assert!(allocator.allocate(&mut aligned_data.0).is_some());
        assert!(allocator.allocate(&mut aligned_data.0).is_some());
        assert!(allocator.allocate(&mut aligned_data.0).is_some());
        assert!(allocator.allocate(&mut aligned_data.0).is_some());

        assert!(allocator
            .allocate_specific(&mut aligned_data.0, 510)
            .is_ok());
        assert!(allocator
            .allocate_specific(&mut aligned_data.0, 542)
            .is_ok());

        let allocated_ids: Vec<u16> = allocator.iter_allocated(&aligned_data.0).collect();
        assert_eq!(allocated_ids.len(), 6);
        assert_eq!(allocated_ids[0], 500);
        assert_eq!(allocated_ids[1], 501);
        assert_eq!(allocated_ids[2], 502);
        assert_eq!(allocated_ids[3], 503);
        assert_eq!(allocated_ids[4], 510);
        assert_eq!(allocated_ids[5], 542);

        assert!(allocator.deallocate(&mut aligned_data.0, 501));
        assert!(allocator.deallocate(&mut aligned_data.0, 503));

        let allocated_ids: Vec<u16> = allocator.iter_allocated(&aligned_data.0).collect();
        assert_eq!(allocated_ids.len(), 4);
        assert_eq!(allocated_ids[0], 500);
        assert_eq!(allocated_ids[1], 502);
        assert_eq!(allocated_ids[2], 510);
        assert_eq!(allocated_ids[3], 542);
    }
}
