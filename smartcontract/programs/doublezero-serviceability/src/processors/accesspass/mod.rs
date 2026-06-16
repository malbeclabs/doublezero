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

/// Default per-user airdrop seeded into `GlobalState.user_airdrop_lamports` at initialization.
/// Admins can override it via the `SetAirdrop` instruction.
pub const DEFAULT_USER_AIRDROP_LAMPORTS: u64 = 40_000;

/// Computes the target lamport balance a `user_payer` must hold to cover rent for its `User`
/// accounts plus the configured per-user airdrop. `multiplier` scales the target for passes that
/// admit several users (e.g. `allow_multiple_ip` seat keypairs); pass `1` for single-user passes.
///
/// Off-chain callers (e.g. the feed oracle) can obtain `Rent` over RPC — by fetching the rent
/// sysvar account, or via `getMinimumBalanceForRentExemption` — since `Rent::get()` is a syscall
/// only available inside a running program.
pub fn airdrop_user_target_lamports(
    rent: &Rent,
    user_airdrop_lamports: u64,
    multiplier: u64,
) -> u64 {
    rent.minimum_balance(AIRDROP_USER_RENT_LAMPORTS_BYTES)
        .saturating_add(user_airdrop_lamports)
        .saturating_mul(multiplier)
}

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
    let target = airdrop_user_target_lamports(&Rent::get()?, user_airdrop_lamports, multiplier);
    let deposit = target.saturating_sub(user_payer.lamports());

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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_airdrop_user_target_lamports() {
        let rent = Rent::default();
        let base = rent.minimum_balance(AIRDROP_USER_RENT_LAMPORTS_BYTES);
        let airdrop = 40_000;

        assert_eq!(
            airdrop_user_target_lamports(&rent, airdrop, 1),
            base + airdrop
        );
        assert_eq!(
            airdrop_user_target_lamports(&rent, airdrop, 5),
            (base + airdrop) * 5
        );
    }
}
