mod builder_stake;
mod program_config;

pub use builder_stake::*;
pub use program_config::*;

//

use solana_pubkey::Pubkey;

use crate::ID;

/// Seed prefix for a 2Z token account owned by one of this program's PDAs. The same shape
/// `revenue-distribution` uses, so an operator reading either program finds the vault the same way.
pub const TOKEN_2Z_PDA_SEED_PREFIX: &[u8] = b"2z_token";

pub fn find_2z_token_pda_address(token_owner: &Pubkey) -> (Pubkey, u8) {
    Pubkey::find_program_address(&[TOKEN_2Z_PDA_SEED_PREFIX, token_owner.as_ref()], &ID)
}

pub fn checked_2z_token_pda_address(token_owner: &Pubkey, bump_seed: u8) -> Option<Pubkey> {
    Pubkey::create_program_address(
        &[TOKEN_2Z_PDA_SEED_PREFIX, token_owner.as_ref(), &[bump_seed]],
        &ID,
    )
    .ok()
}
