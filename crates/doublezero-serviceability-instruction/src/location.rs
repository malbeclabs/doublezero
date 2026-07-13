//! Location-domain instruction builders.
//!
//! The simple `account_index`-seeded CRUD template: every instruction's
//! account list is `[location, globalstate]` (create derives the location PDA
//! from the new index; the rest take the location pubkey). All route through
//! `authorize()` -> [`common::build_with_permission`].

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalstate_pda, get_location_pda},
    processors::location::{
        create::LocationCreateArgs, delete::LocationDeleteArgs, resume::LocationResumeArgs,
        suspend::LocationSuspendArgs, update::LocationUpdateArgs,
    },
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// `CreateLocation` (variant 10). Accounts: `[location, globalstate]`.
///
/// `account_index` is the new location's index (`globalstate.account_index + 1`).
pub fn create_location(
    program_id: &Pubkey,
    payer: &Pubkey,
    account_index: u128,
    args: LocationCreateArgs,
) -> Instruction {
    let (location, _) = get_location_pda(program_id, account_index);
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CreateLocation(args),
        vec![
            AccountMeta::new(location, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `UpdateLocation` (variant 11). Accounts: `[location, globalstate]`.
pub fn update_location(
    program_id: &Pubkey,
    payer: &Pubkey,
    location: &Pubkey,
    args: LocationUpdateArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::UpdateLocation(args),
        vec![
            AccountMeta::new(*location, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `SuspendLocation` (variant 12). Accounts: `[location, globalstate]`.
pub fn suspend_location(
    program_id: &Pubkey,
    payer: &Pubkey,
    location: &Pubkey,
    args: LocationSuspendArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::SuspendLocation(args),
        vec![
            AccountMeta::new(*location, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `ResumeLocation` (variant 13). Accounts: `[location, globalstate]`.
pub fn resume_location(
    program_id: &Pubkey,
    payer: &Pubkey,
    location: &Pubkey,
    args: LocationResumeArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::ResumeLocation(args),
        vec![
            AccountMeta::new(*location, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `DeleteLocation` (variant 14). Accounts: `[location, globalstate]`.
pub fn delete_location(
    program_id: &Pubkey,
    payer: &Pubkey,
    location: &Pubkey,
    args: LocationDeleteArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::DeleteLocation(args),
        vec![
            AccountMeta::new(*location, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_system_interface::program as system_program;

    fn create_args() -> LocationCreateArgs {
        LocationCreateArgs {
            code: "loc".to_string(),
            name: "Location".to_string(),
            country: "US".to_string(),
            lat: 1.0,
            lng: 2.0,
            loc_id: 3,
        }
    }

    #[test]
    fn test_create_location() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let ix = create_location(&pid, &payer, 1, create_args());
        assert_eq!(ix.data[0], 10);
        let (location, _) = get_location_pda(&pid, 1);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(location, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_location_pubkey_verbs() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let location = Pubkey::new_unique();
        let (globalstate, _) = get_globalstate_pda(&pid);
        let expected = vec![
            AccountMeta::new(location, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(payer, true),
            AccountMeta::new(system_program::ID, false),
        ];

        for (ix, tag) in [
            (
                update_location(&pid, &payer, &location, LocationUpdateArgs::default()),
                11,
            ),
            (
                suspend_location(&pid, &payer, &location, LocationSuspendArgs {}),
                12,
            ),
            (
                resume_location(&pid, &payer, &location, LocationResumeArgs {}),
                13,
            ),
            (
                delete_location(&pid, &payer, &location, LocationDeleteArgs {}),
                14,
            ),
        ] {
            assert_eq!(ix.data[0], tag);
            assert_eq!(ix.accounts, expected);
        }
    }
}
