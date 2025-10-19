use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_tools::{Discriminator, PrecomputedDiscriminator};
use solana_pubkey::Pubkey;

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, PartialEq, Eq)]
pub struct ProgramState {
    pub admin_key: Pubkey,
    pub fills_registry_key: Pubkey,
    pub is_paused: bool,
    pub configuration_registry_bump: u8,
    pub program_state_bump: u8,
    pub deny_list_registry_bump: u8,
    pub withdraw_authority_bump: u8,
    pub last_trade_slot: u64,
    pub deny_list_authority: Pubkey,
}

impl PrecomputedDiscriminator for ProgramState {
    const DISCRIMINATOR: Discriminator<8> = Discriminator::new_sha2(b"account:ProgramStateAccount");
}

impl ProgramState {
    pub const SEED_PREFIX: &'static [u8] = b"state";

    pub fn find_address() -> (Pubkey, u8) {
        Pubkey::find_program_address(&[Self::SEED_PREFIX], &crate::ID)
    }
}
