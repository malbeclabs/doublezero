use crate::state::accounttype::AccountType;
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::types::NetworkV4;
use solana_program::{account_info::AccountInfo, program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub struct GlobalConfig {
    pub account_type: AccountType,       // 1
    pub owner: Pubkey,                   // 32
    pub bump_seed: u8,                   // 1
    pub local_asn: u32,                  // 4
    pub remote_asn: u32,                 // 4
    pub device_tunnel_block: NetworkV4,  // 5
    pub user_tunnel_block: NetworkV4,    // 5
    pub multicastgroup_block: NetworkV4, // 5
}

impl fmt::Display for GlobalConfig {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, local_asn: {}, remote_asn: {}, device_tunnel_block: {}, user_tunnel_block: {}, multicastgroup_block: {}",
            self.account_type, self.owner, self.local_asn, self.remote_asn,
            &self.device_tunnel_block,
            &self.user_tunnel_block,
            &self.multicastgroup_block,
        )
    }
}

impl TryFrom<&[u8]> for GlobalConfig {
    type Error = ProgramError;

    fn try_from(mut data: &[u8]) -> Result<Self, Self::Error> {
        let out = Self {
            account_type: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            owner: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            bump_seed: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            local_asn: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            remote_asn: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            device_tunnel_block: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            user_tunnel_block: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
            multicastgroup_block: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::Config {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for GlobalConfig {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        Self::try_from(&data[..])
    }
}

impl GlobalConfig {
    pub fn size(&self) -> usize {
        1 + 32 + 1 + 4 + 4 + 5 + 5 + 5
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_globalconfig_serialization() {
        let val = GlobalConfig {
            account_type: AccountType::Config,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            local_asn: 123,
            remote_asn: 456,
            device_tunnel_block: "10.0.0.1/24".parse().unwrap(),
            user_tunnel_block: "10.0.0.2/24".parse().unwrap(),
            multicastgroup_block: "224.0.0.0/4".parse().unwrap(),
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = borsh::from_slice::<GlobalConfig>(&data).unwrap();

        assert_eq!(val.size(), val2.size());
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.local_asn, val2.local_asn);
        assert_eq!(val.remote_asn, val2.remote_asn);
        assert_eq!(val.device_tunnel_block, val2.device_tunnel_block);
        assert_eq!(val.user_tunnel_block, val2.user_tunnel_block);
        assert_eq!(val.multicastgroup_block, val2.multicastgroup_block);
        assert_eq!(data.len(), val.size(), "Invalid Size");
    }
}
