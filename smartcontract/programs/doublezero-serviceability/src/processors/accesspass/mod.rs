pub mod check_status;
pub mod close;
pub mod set;

use solana_program::{
    account_info::AccountInfo, entrypoint::ProgramResult, msg, program::invoke_signed_unchecked,
    rent::Rent, sysvar::Sysvar,
};

// Value to rent exempt three `User` accounts + configurable amount for connect/disconnect txns.
// `User` account size assumes a single publisher and subscriber pubkey registered (266 bytes each).
pub const AIRDROP_USER_RENT_LAMPORTS_BYTES: usize = 266 * 3; // 266 bytes per User account x 3 accounts = 798 bytes

/// Tops up `user_payer` so it holds enough SOL to cover rent for its `User` accounts plus the
/// configured per-user airdrop, allowing the user to connect immediately. `multiplier` scales the
/// target for passes that admit several users (e.g. `allow_multiple_ip` seat keypairs); pass `1`
/// for single-user passes. The transfer is funded by `payer_account` and is a no-op when
/// `user_payer` already meets the target. Performed via CPI so a failed transfer reverts the whole
/// instruction (keeping the operation atomic with the surrounding account writes).
pub fn airdrop_user_credits<'a>(
    payer_account: &AccountInfo<'a>,
    user_payer: &AccountInfo<'a>,
    system_program: &AccountInfo<'a>,
    user_airdrop_lamports: u64,
    multiplier: u64,
) -> ProgramResult {
    let base_target = Rent::get()?
        .minimum_balance(AIRDROP_USER_RENT_LAMPORTS_BYTES)
        .saturating_add(user_airdrop_lamports);
    let deposit = base_target
        .saturating_mul(multiplier)
        .saturating_sub(user_payer.lamports());

    if deposit == 0 {
        return Ok(());
    }

    msg!("Airdropping {} lamports to user account", deposit);
    invoke_signed_unchecked(
        &solana_system_interface::instruction::transfer(payer_account.key, user_payer.key, deposit),
        &[
            payer_account.clone(),
            user_payer.clone(),
            system_program.clone(),
        ],
        &[],
    )
}
