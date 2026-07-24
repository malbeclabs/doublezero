//! Migrate-domain instruction builder.

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::migrate::MigrateArgs,
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// `Migrate` (variant 0) — migrate one user from its old PDA to the new PDA.
/// Accounts: `[old_user, new_user]`.
///
/// The processor does not call `authorize()`, so this uses [`common::build`].
/// `old_user` is `get_user_old_pda(index)` and `new_user` is
/// `get_user_pda(client_ip, user_type)`; both are resolved by the caller.
pub fn migrate(
    program_id: &Pubkey,
    payer: &Pubkey,
    old_user: &Pubkey,
    new_user: &Pubkey,
    args: MigrateArgs,
) -> Instruction {
    common::build(
        program_id,
        DoubleZeroInstruction::Migrate(args),
        vec![
            AccountMeta::new(*old_user, false),
            AccountMeta::new(*new_user, false),
        ],
        payer,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_system_interface::program as system_program;

    #[test]
    fn test_migrate_uses_build() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let old_user = Pubkey::new_unique();
        let new_user = Pubkey::new_unique();
        let ix = migrate(&pid, &payer, &old_user, &new_user, MigrateArgs {});
        assert_eq!(ix.data[0], 0);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(old_user, false),
                AccountMeta::new(new_user, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }
}
