use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::pubkey::Pubkey;

#[derive(BorshSerialize, BorshDeserialize, Clone, Copy, Debug, Default, PartialEq)]
pub enum IpBlockType {
    #[default]
    DeviceTunnelBlock,
    UserTunnelBlock,
    MulticastGroupBlock,
    DzPrefixBlock(Pubkey, usize),
}
