//! Index-domain instruction builders.
//!
//! Both route through `authorize()` -> [`common::build_with_permission`].

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalstate_pda, get_index_pda},
    processors::index::{create::IndexCreateArgs, delete::IndexDeleteArgs},
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// `CreateIndex` (variant 104). Accounts: `[index, entity(readonly), globalstate(readonly)]`.
///
/// The index PDA is derived from `args.entity_seed` and `args.key`.
pub fn create_index(
    program_id: &Pubkey,
    payer: &Pubkey,
    entity: &Pubkey,
    args: IndexCreateArgs,
) -> Instruction {
    let (index, _) = get_index_pda(program_id, args.entity_seed.as_bytes(), &args.key);
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CreateIndex(args),
        vec![
            AccountMeta::new(index, false),
            AccountMeta::new_readonly(*entity, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        payer,
    )
}

/// `DeleteIndex` (variant 105). Accounts: `[index, globalstate(readonly)]`.
pub fn delete_index(
    program_id: &Pubkey,
    payer: &Pubkey,
    index: &Pubkey,
    args: IndexDeleteArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::DeleteIndex(args),
        vec![
            AccountMeta::new(*index, false),
            AccountMeta::new_readonly(globalstate, false),
        ],
        payer,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_system_interface::program as system_program;

    #[test]
    fn test_create_index() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let entity = Pubkey::new_unique();
        let args = IndexCreateArgs {
            entity_seed: "device".to_string(),
            key: "abc".to_string(),
        };
        let ix = create_index(&pid, &payer, &entity, args);
        assert_eq!(ix.data[0], 104);
        let (index, _) = get_index_pda(&pid, b"device", "abc");
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(index, false),
                AccountMeta::new_readonly(entity, false),
                AccountMeta::new_readonly(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_delete_index() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let index = Pubkey::new_unique();
        let ix = delete_index(&pid, &payer, &index, IndexDeleteArgs {});
        assert_eq!(ix.data[0], 105);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(index, false),
                AccountMeta::new_readonly(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }
}
