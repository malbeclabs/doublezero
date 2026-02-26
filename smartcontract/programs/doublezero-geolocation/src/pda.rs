use solana_program::pubkey::Pubkey;

use crate::seeds::{SEED_PREFIX, SEED_PROGRAM_CONFIG};

pub fn get_program_config_pda(program_id: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(&[SEED_PREFIX, SEED_PROGRAM_CONFIG], program_id)
}
