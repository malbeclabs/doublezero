//! GlobalState-domain instruction builders.
//!
//! `init_global_state` never calls `authorize()` (it bootstraps GlobalState) so it
//! uses [`common::build`]; the setters route through `authorize()` ->
//! [`common::build_with_permission`].

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalstate_pda, get_program_config_pda},
    processors::globalstate::{
        setairdrop::SetAirdropArgs, setauthority::SetAuthorityArgs,
        setfeatureflags::SetFeatureFlagsArgs, setversion::SetVersionArgs,
    },
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// `InitGlobalState` (variant 1). Accounts: `[program_config, globalstate]`.
pub fn init_global_state(program_id: &Pubkey, payer: &Pubkey) -> Instruction {
    let (program_config, _) = get_program_config_pda(program_id);
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build(
        program_id,
        DoubleZeroInstruction::InitGlobalState(),
        vec![
            AccountMeta::new(program_config, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `SetAuthority` (variant 2). Accounts: `[globalstate]`.
pub fn set_authority(program_id: &Pubkey, payer: &Pubkey, args: SetAuthorityArgs) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::SetAuthority(args),
        vec![AccountMeta::new(globalstate, false)],
        payer,
    )
}

/// `SetAirdrop` (variant 68). Accounts: `[globalstate]`.
pub fn set_airdrop(program_id: &Pubkey, payer: &Pubkey, args: SetAirdropArgs) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::SetAirdrop(args),
        vec![AccountMeta::new(globalstate, false)],
        payer,
    )
}

/// `SetMinVersion` (variant 79). Accounts: `[globalstate]`.
pub fn set_min_version(program_id: &Pubkey, payer: &Pubkey, args: SetVersionArgs) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::SetMinVersion(args),
        vec![AccountMeta::new(globalstate, false)],
        payer,
    )
}

/// `SetFeatureFlags` (variant 94). Accounts: `[globalstate]`.
pub fn set_feature_flags(
    program_id: &Pubkey,
    payer: &Pubkey,
    args: SetFeatureFlagsArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::SetFeatureFlags(args),
        vec![AccountMeta::new(globalstate, false)],
        payer,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_system_interface::program as system_program;

    #[test]
    fn test_init_global_state_uses_build() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let ix = init_global_state(&pid, &payer);
        assert_eq!(ix.data[0], 1);
        let (program_config, _) = get_program_config_pda(&pid);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(program_config, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_globalstate_setters() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let (globalstate, _) = get_globalstate_pda(&pid);
        let expected = vec![
            AccountMeta::new(globalstate, false),
            AccountMeta::new(payer, true),
            AccountMeta::new(system_program::ID, false),
        ];
        for (ix, tag) in [
            (set_authority(&pid, &payer, SetAuthorityArgs::default()), 2),
            (
                set_airdrop(
                    &pid,
                    &payer,
                    SetAirdropArgs {
                        contributor_airdrop_lamports: None,
                        user_airdrop_lamports: None,
                    },
                ),
                68,
            ),
            (set_min_version(&pid, &payer, SetVersionArgs::default()), 79),
            (
                set_feature_flags(&pid, &payer, SetFeatureFlagsArgs { feature_flags: 0 }),
                94,
            ),
        ] {
            assert_eq!(ix.data[0], tag);
            assert_eq!(ix.accounts, expected);
        }
    }
}
