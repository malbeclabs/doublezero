use bitvec::prelude::*;
use double_zero_sdk::{networkv4_to_ipnetwork, NetworkV4};
use ipnetwork::Ipv4Network;

#[derive(Debug)]
pub struct IPBlockAllocator {
    pub base_block: Ipv4Network,
    pub assigned_ips: BitVec,
    total_ips: usize,
}

impl IPBlockAllocator {
    /// Creates a new IPBlockAllocator with the given base block.
    /// Initializes the bit vector to track assigned IPs.
    pub fn new(base_block: NetworkV4) -> Self {
        let base_block = networkv4_to_ipnetwork(&base_block);
        let total_ips = 2_usize.pow((32 - base_block.prefix()).into());
        Self {
            base_block,
            assigned_ips: bitvec![0; total_ips],
            total_ips,
        }
    }

    /// Marks the given block of IPs as assigned.
    /// Updates the bit vector to reflect the assigned IPs.
    pub fn assign_block(&mut self, block: NetworkV4) {
        let block = networkv4_to_ipnetwork(&block);
        match self.ip_to_index(block.ip()) {
            Ok(start_index) => {
                let block_size = 2_usize.pow((32 - block.prefix()).into());

                for i in start_index..start_index + block_size {
                    self.assigned_ips.set(i, true);
                }
            }
            Err(e) => {
                print!(" {} ", e);
            }
        }
    }

    /// Marks the given block of IPs as assigned.
    /// Updates the bit vector to reflect the assigned IPs.
    pub fn unassign_block(&mut self, block: NetworkV4) {
        let block = networkv4_to_ipnetwork(&block);
        match self.ip_to_index(block.ip()) {
            Ok(start_index) => {
                let block_size = 2_usize.pow((32 - block.prefix()).into());

                for i in start_index..start_index + block_size {
                    self.assigned_ips.set(i, false);
                }
            }
            Err(e) => {
                print!(" {} ", e);
            }
        }
    }

    /// Finds the next available block of IPs that can accommodate the given number of IPs.
    /// Returns an Ipv4Network representing the available block, or None if no block is available.
    pub fn next_available_block(&mut self, reserve: usize, ip_count: usize) -> Option<NetworkV4> {
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

                self.assign_block((start_ip.octets(), block_prefix));
                return Some((start_ip.octets(), block_prefix));
            }

            start_index += block_size;
        }

        None
    }

    pub fn print_assigned_ips(&self) {
        for (index, assigned) in self.assigned_ips.iter().enumerate() {
            if *assigned {
                let ip = self.index_to_ip(index);
                print!("{},", ip.to_string());
            }
        }
    }

    /// Converts an IP address to an index in the bit vector.
    fn ip_to_index(&self, ip: std::net::Ipv4Addr) -> Result<usize, &'static str> {
        let base_ip = u32::from(self.base_block.network());
        let ip_as_u32 = u32::from(ip);

        if base_ip <= ip_as_u32 {
            Ok((ip_as_u32 - base_ip) as usize)
        } else {
            return Err("IP address is not in the base block");
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
        let block1 = IPBlockAllocator::new(([10, 0, 0, 1], 24))
            .next_available_block(1, 1)
            .unwrap();
        assert_eq!(block1, ([10, 0, 0, 1], 32));

        let block1 = IPBlockAllocator::new(([10, 0, 0, 1], 24))
            .next_available_block(1, 2)
            .unwrap();
        assert_eq!(block1, ([10, 0, 0, 1], 31));

        let block1 = IPBlockAllocator::new(([10, 0, 0, 1], 24))
            .next_available_block(1, 1)
            .unwrap();
        assert_eq!(block1, ([10, 0, 0, 1], 32));

        let block1 = IPBlockAllocator::new(([10, 0, 0, 1], 24))
            .next_available_block(2, 4)
            .unwrap();
        assert_eq!(block1, ([10, 0, 0, 2], 30));
    }
}
