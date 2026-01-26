use std::net::Ipv4Addr;

use bitvec::prelude::*;
use ipnetwork::Ipv4Network;
use log::warn;

#[derive(Debug)]
pub struct IPBlockAllocator {
    pub base_block: Ipv4Network,
    pub assigned_ips: BitVec,
    pub total_ips: usize,
}

impl IPBlockAllocator {
    /// Creates a new IPBlockAllocator with the given base block.
    /// Initializes the bit vector to track assigned IPs.
    pub fn new(base_block: Ipv4Network) -> Self {
        let total_ips = 2_usize.pow((32 - base_block.prefix()).into());
        Self {
            base_block,
            assigned_ips: bitvec![0; total_ips],
            total_ips,
        }
    }

    pub fn contains(&self, ip: Ipv4Addr) -> bool {
        let base_ip = u32::from(self.base_block.network());
        let ip_as_u32 = u32::from(ip);

        base_ip <= ip_as_u32 && ip_as_u32 < base_ip + self.total_ips as u32
    }

    /// Marks the given block of IPs as assigned.
    /// Updates the bit vector to reflect the assigned IPs.
    pub fn assign_block(&mut self, block: Ipv4Network) {
        match self.ip_to_index(block.ip()) {
            Ok(start_index) => {
                let block_size = 2_usize.pow((32 - block.prefix()).into());

                for i in start_index..start_index + block_size {
                    self.assigned_ips.set(i, true);
                }
            }
            Err(e) => {
                warn!(" {e} ");
            }
        }
    }

    /// Marks the given block of IPs as unassigned.
    /// Updates the bit vector to reflect the unassigned IPs.
    pub fn unassign_block(&mut self, block: Ipv4Network) {
        match self.ip_to_index(block.ip()) {
            Ok(start_index) => {
                let block_size = 2_usize.pow((32 - block.prefix()).into());

                for i in start_index..start_index + block_size {
                    self.assigned_ips.set(i, false);
                }
            }
            Err(e) => {
                warn!(" {e} ");
            }
        }
    }

    /// Finds the next available block of IPs that can accommodate the given number of IPs.
    /// Returns an Ipv4Network representing the available block, or None if no block is available.
    pub fn next_available_block(&mut self, reserve: usize, ip_count: usize) -> Option<Ipv4Network> {
        let block_prefix =
            (32 - (ip_count as f32).log2().ceil() as u8).max(self.base_block.prefix());
        let block_size = 2_usize.pow((32 - block_prefix).into());

        let mut start_index = reserve;

        if self.base_block.prefix() == 32 {
            start_index = 0;
        }

        while start_index + block_size <= self.total_ips {
            let range = &self.assigned_ips[start_index..(start_index + block_size)];
            if range.not_any() {
                for i in start_index..(start_index + block_size) {
                    self.assigned_ips.set(i, true);
                }
                let start_ip = self.index_to_ip(start_index);

                let next_block = Ipv4Network::new(start_ip, block_prefix).ok()?;
                return Some(next_block);
            }

            start_index += block_size;
        }

        None
    }

    #[allow(dead_code)]
    pub fn display_assigned_ips(&self) -> String {
        let mut ips = String::new();
        for (index, assigned) in self.assigned_ips.iter().enumerate() {
            if *assigned {
                let ip = self.index_to_ip(index);
                ips.push_str(&format!("{ip},"));
            }
        }
        ips.trim_end_matches(',').to_string()
    }

    /// Converts an IP address to an index in the bit vector.
    /// Returns an error if the IP is outside the base block range.
    fn ip_to_index(&self, ip: std::net::Ipv4Addr) -> Result<usize, &'static str> {
        if self.contains(ip) {
            let base_ip = u32::from(self.base_block.network());
            let ip_as_u32 = u32::from(ip);
            Ok((ip_as_u32 - base_ip) as usize)
        } else {
            Err("IP address is not in the base block")
        }
    }

    fn index_to_ip(&self, index: usize) -> std::net::Ipv4Addr {
        let base_ip = u32::from(self.base_block.network());
        std::net::Ipv4Addr::from(base_ip + index as u32)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_ipallocation() {
        let block1 = IPBlockAllocator::new("10.0.0.1/24".parse().unwrap())
            .next_available_block(1, 1)
            .unwrap();
        assert_eq!(block1, "10.0.0.1/32".parse().unwrap());

        let block1 = IPBlockAllocator::new("10.0.0.1/24".parse().unwrap())
            .next_available_block(1, 2)
            .unwrap();
        assert_eq!(block1, "10.0.0.1/31".parse().unwrap());

        let block1 = IPBlockAllocator::new("10.0.0.1/24".parse().unwrap())
            .next_available_block(1, 1)
            .unwrap();
        assert_eq!(block1, "10.0.0.1/32".parse().unwrap());

        let block1 = IPBlockAllocator::new("10.0.0.1/24".parse().unwrap())
            .next_available_block(2, 4)
            .unwrap();
        assert_eq!(block1, "10.0.0.2/30".parse().unwrap());
    }

    #[test]
    fn test_unassign_block_ip_outside_pool_does_not_panic() {
        // Reproduces bug: activator panicked when deleting Loopback100 interface
        // with public IP (195.219.121.96) outside the tunnel pool (172.16.0.0/16).
        // The ip_to_index function only checked lower bound, causing bitvec panic.
        let mut allocator = IPBlockAllocator::new("172.16.0.0/16".parse().unwrap());

        // IP above pool (195.219.121.96 > 172.16.255.255) should not panic
        let high_ip: Ipv4Network = "195.219.121.96/32".parse().unwrap();
        allocator.unassign_block(high_ip); // Should log warning, not panic

        // IP below pool (70.70.70.70 < 172.16.0.0) should not panic
        let low_ip: Ipv4Network = "70.70.70.70/32".parse().unwrap();
        allocator.unassign_block(low_ip); // Should log warning, not panic

        // Verify pool is unchanged (no IPs were actually unassigned)
        assert_eq!(allocator.assigned_ips.count_ones(), 0);
    }

    #[test]
    fn test_ip_to_index_validates_upper_bound() {
        let allocator = IPBlockAllocator::new("172.16.0.0/16".parse().unwrap());

        // IP above pool should return error
        let result = allocator.ip_to_index("195.219.121.96".parse().unwrap());
        assert!(result.is_err());

        // IP at upper boundary (172.17.0.0) should return error
        let result = allocator.ip_to_index("172.17.0.0".parse().unwrap());
        assert!(result.is_err());

        // IP at max valid index (172.16.255.255) should succeed
        let result = allocator.ip_to_index("172.16.255.255".parse().unwrap());
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), 65535);

        // IP at base should succeed
        let result = allocator.ip_to_index("172.16.0.0".parse().unwrap());
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), 0);
    }
}
