use crate::{
    error::{DoubleZeroError, Validate},
    state::{accounttype::AccountType, exchange::BGP_COMMUNITY_MIN},
};
use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_common::types::NetworkV4;
use solana_program::{account_info::AccountInfo, msg, program_error::ProgramError, pubkey::Pubkey};
use std::fmt;

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
#[cfg_attr(feature = "serde", derive(serde::Serialize, serde::Deserialize))]
pub struct GlobalConfig {
    pub account_type: AccountType,            // 1
    pub owner: Pubkey,                        // 32
    pub bump_seed: u8,                        // 1
    pub local_asn: u32,                       // 4
    pub remote_asn: u32,                      // 4
    pub device_tunnel_block: NetworkV4,       // 5
    pub user_tunnel_block: NetworkV4,         // 5
    pub multicastgroup_block: NetworkV4,      // 5
    pub next_bgp_community: u16,              // 2
    pub multicast_publisher_block: NetworkV4, // 5
}

impl fmt::Display for GlobalConfig {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "account_type: {}, owner: {}, local_asn: {}, remote_asn: {}, device_tunnel_block: {}, user_tunnel_block: {}, multicastgroup_block: {}, next_bgp_community: {}, multicast_publisher_block: {}",
            self.account_type, self.owner, self.local_asn, self.remote_asn,
            &self.device_tunnel_block,
            &self.user_tunnel_block,
            &self.multicastgroup_block,
            self.next_bgp_community,
            &self.multicast_publisher_block,
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
            next_bgp_community: BorshDeserialize::deserialize(&mut data)
                .unwrap_or(BGP_COMMUNITY_MIN),
            multicast_publisher_block: BorshDeserialize::deserialize(&mut data).unwrap_or_default(),
        };

        if out.account_type != AccountType::GlobalConfig {
            return Err(ProgramError::InvalidAccountData);
        }

        Ok(out)
    }
}

impl TryFrom<&AccountInfo<'_>> for GlobalConfig {
    type Error = ProgramError;

    fn try_from(account: &AccountInfo) -> Result<Self, Self::Error> {
        let data = account.try_borrow_data()?;
        let res = Self::try_from(&data[..]);
        if res.is_err() {
            msg!(
                "Failed to deserialize GlobalConfig: {:?}",
                res.as_ref().err()
            );
        }
        res
    }
}

impl GlobalConfig {
    pub fn size(&self) -> usize {
        1 + 32 + 1 + 4 + 4 + 5 + 5 + 5 + 2 + 5
    }
}

impl Validate for GlobalConfig {
    fn validate(&self) -> Result<(), DoubleZeroError> {
        // Account type must be GlobalConfig
        if self.account_type != AccountType::GlobalConfig {
            msg!("Invalid account type: {}", self.account_type);
            return Err(DoubleZeroError::InvalidAccountType);
        }
        if self.local_asn == 0 || self.local_asn > 4294967294 {
            msg!("Invalid local ASN: {}", self.local_asn);
            return Err(DoubleZeroError::InvalidLocalAsn);
        }
        if self.remote_asn == 0 || self.remote_asn > 4294967294 {
            msg!("Invalid remote ASN: {}", self.remote_asn);
            return Err(DoubleZeroError::InvalidRemoteAsn);
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_state_compatibility_globalconfig() {
        /* To generate the base64 strings, use the following commands after deploying the program and creating accounts:

        solana account 8uA3su1WQPXvnhN5DhNueXrmizA8CY7dE5sj1vXkAWnf --output json  -u  https://doublezerolocalnet.rpcpool.com/8a4fd3f4-0977-449f-88c7-63d4b0f10f16

         */
        let versions =
            ["ArqURkOjUnp/ZIYOxBHg7ts7n0lFlaGFNKiKe+P8gnOq/uj9AAA+/wAArBAAABCp/gAAEOlUsgAY"];

        crate::helper::base_tests::test_parsing::<GlobalConfig>(&versions).unwrap();
    }

    #[test]
    fn test_state_globalconfig_try_from_defaults() {
        let data = [AccountType::GlobalConfig as u8];
        let val = GlobalConfig::try_from(&data[..]).unwrap();

        assert_eq!(val.owner, Pubkey::default());
        assert_eq!(val.bump_seed, 0);
        assert_eq!(val.local_asn, 0);
        assert_eq!(val.remote_asn, 0);
        assert_eq!(val.device_tunnel_block, NetworkV4::default());
        assert_eq!(val.user_tunnel_block, NetworkV4::default());
        assert_eq!(val.multicastgroup_block, NetworkV4::default());
        assert_eq!(val.next_bgp_community, BGP_COMMUNITY_MIN);
        assert_eq!(val.multicast_publisher_block, NetworkV4::default());
    }

    #[test]
    fn test_state_globalconfig_serialization() {
        let val = GlobalConfig {
            account_type: AccountType::GlobalConfig,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            local_asn: 123,
            remote_asn: 456,
            device_tunnel_block: "10.0.0.1/24".parse().unwrap(),
            user_tunnel_block: "10.0.0.2/24".parse().unwrap(),
            multicastgroup_block: "224.0.0.0/4".parse().unwrap(),
            next_bgp_community: BGP_COMMUNITY_MIN,
            multicast_publisher_block: "147.51.126.0/23".parse().unwrap(),
        };

        let data = borsh::to_vec(&val).unwrap();
        let val2 = borsh::from_slice::<GlobalConfig>(&data).unwrap();

        val.validate().unwrap();
        val2.validate().unwrap();

        assert_eq!(
            borsh::object_length(&val).unwrap(),
            borsh::object_length(&val2).unwrap()
        );
        assert_eq!(val.owner, val2.owner);
        assert_eq!(val.local_asn, val2.local_asn);
        assert_eq!(val.remote_asn, val2.remote_asn);
        assert_eq!(val.device_tunnel_block, val2.device_tunnel_block);
        assert_eq!(val.user_tunnel_block, val2.user_tunnel_block);
        assert_eq!(val.multicastgroup_block, val2.multicastgroup_block);
        assert_eq!(val.next_bgp_community, val2.next_bgp_community);
        assert_eq!(
            val.multicast_publisher_block,
            val2.multicast_publisher_block
        );
        assert_eq!(
            data.len(),
            borsh::object_length(&val).unwrap(),
            "Invalid Size"
        );
    }

    #[test]
    fn test_state_globalconfig_validate_error_invalid_account_type() {
        let val = GlobalConfig {
            account_type: AccountType::Device, // Should be GlobalConfig
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            local_asn: 123,
            remote_asn: 456,
            device_tunnel_block: "10.0.0.1/24".parse().unwrap(),
            user_tunnel_block: "10.0.0.2/24".parse().unwrap(),
            multicastgroup_block: "224.0.0.0/4".parse().unwrap(),
            next_bgp_community: BGP_COMMUNITY_MIN,
            multicast_publisher_block: "147.51.126.0/23".parse().unwrap(),
        };
        let err = val.validate();
        assert!(err.is_err());
        assert_eq!(err.unwrap_err(), DoubleZeroError::InvalidAccountType);
    }

    #[test]
    fn test_state_globalconfig_validate_error_invalid_local_asn() {
        let val_zero = GlobalConfig {
            account_type: AccountType::GlobalConfig,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            local_asn: 0, // Invalid
            remote_asn: 456,
            device_tunnel_block: "10.0.0.1/24".parse().unwrap(),
            user_tunnel_block: "10.0.0.2/24".parse().unwrap(),
            multicastgroup_block: "224.0.0.0/4".parse().unwrap(),
            next_bgp_community: BGP_COMMUNITY_MIN,
            multicast_publisher_block: "147.51.126.0/23".parse().unwrap(),
        };
        let err_zero = val_zero.validate();
        assert!(err_zero.is_err());
        assert_eq!(err_zero.unwrap_err(), DoubleZeroError::InvalidLocalAsn);

        let val_high = GlobalConfig {
            local_asn: 4294967295, // Invalid
            ..val_zero
        };
        let err_high = val_high.validate();
        assert!(err_high.is_err());
        assert_eq!(err_high.unwrap_err(), DoubleZeroError::InvalidLocalAsn);
    }

    #[test]
    fn test_state_globalconfig_validate_error_invalid_remote_asn() {
        let val_zero = GlobalConfig {
            account_type: AccountType::GlobalConfig,
            owner: Pubkey::new_unique(),
            bump_seed: 1,
            local_asn: 123,
            remote_asn: 0, // Invalid
            device_tunnel_block: "10.0.0.1/24".parse().unwrap(),
            user_tunnel_block: "10.0.0.2/24".parse().unwrap(),
            multicastgroup_block: "224.0.0.0/4".parse().unwrap(),
            next_bgp_community: BGP_COMMUNITY_MIN,
            multicast_publisher_block: "147.51.126.0/23".parse().unwrap(),
        };
        let err_zero = val_zero.validate();
        assert!(err_zero.is_err());
        assert_eq!(err_zero.unwrap_err(), DoubleZeroError::InvalidRemoteAsn);

        let val_high = GlobalConfig {
            remote_asn: 4294967295, // Invalid
            ..val_zero
        };
        let err_high = val_high.validate();
        assert!(err_high.is_err());
        assert_eq!(err_high.unwrap_err(), DoubleZeroError::InvalidRemoteAsn);
    }
}
