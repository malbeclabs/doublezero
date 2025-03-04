use std::fmt;

use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::pubkey::Pubkey;

use super::accounttype::AccountType;
use crate::{bytereader::ByteReader, types::{networkv4_to_string, NetworkV4}};

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub struct GlobalConfig {
    pub account_type: AccountType,          // 1
    pub owner: Pubkey,                      // 32
    pub local_asn: u32,                     // 4
    pub remote_asn: u32,                    // 4
    pub tunnel_tunnel_block: NetworkV4,     // 5
    pub user_tunnel_block: NetworkV4,       // 5
}

impl fmt::Display for GlobalConfig {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, local_asn: {}, remote_asn: {}, tunnel_tunnel_block: {}, user_tunnel_block: {}",
            self.account_type, self.owner, self.local_asn, self.remote_asn, networkv4_to_string(&self.tunnel_tunnel_block), 
            networkv4_to_string(&self.user_tunnel_block)
        )
    }
}

impl From<&[u8]> for GlobalConfig {
    fn from(data: &[u8]) -> Self {
        let mut parser = ByteReader::new(data);

        Self {
            account_type: parser.read_enum(),
            owner: parser.read_pubkey(),
            local_asn: parser.read_u32(),
            remote_asn: parser.read_u32(),
            tunnel_tunnel_block: parser.read_networkv4(),
            user_tunnel_block: parser.read_networkv4(),
        }
    }
}

impl GlobalConfig {
    pub fn size(&self) -> usize {
        1 + 32 + 4 + 4 + 5 + 5
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_location_serialization() {
        let val = GlobalConfig {
            account_type: AccountType::Config,
            owner: Pubkey::new_unique(),
            local_asn: 123,
            remote_asn: 456,
            tunnel_tunnel_block: ([10, 0, 0, 1], 24),
            user_tunnel_block: ([10, 0, 0, 2], 24),
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = borsh::from_slice::<GlobalConfig>(&data).unwrap();

        assert_eq!(val.size(), val2.size());
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.local_asn, val2.local_asn);
        assert_eq!(val.remote_asn, val2.remote_asn);
        assert_eq!(val.tunnel_tunnel_block, val2.tunnel_tunnel_block);
        assert_eq!(val.user_tunnel_block, val2.user_tunnel_block);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }
}
