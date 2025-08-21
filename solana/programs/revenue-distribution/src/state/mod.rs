mod contributor_rewards;
mod distribution;
mod journal;
mod prepaid_connection;
mod program_config;
mod solana_validator_deposit;

pub use contributor_rewards::*;
pub use distribution::*;
pub use journal::*;
pub use prepaid_connection::*;
pub use program_config::*;
pub use solana_validator_deposit::*;

//

use solana_pubkey::Pubkey;

use crate::ID;

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
