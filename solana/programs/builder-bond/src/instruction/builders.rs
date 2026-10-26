//! Instruction builders for `builder-bond`.
//!
//! Every builder here fixes the account order the processor expects, so a caller cannot get it
//! wrong and a change to the processor has one place to update rather than one per caller.

use doublezero_program_tools::get_program_data_address;
use solana_instruction::{AccountMeta, Instruction};
use solana_pubkey::Pubkey;
use solana_system_interface::program as system_program;

use crate::{
    instruction::BuilderBondInstructionData,
    state::{self, BuilderBond, ProgramConfig},
    DOUBLEZERO_MINT_KEY, ID,
};

fn encode(data: &BuilderBondInstructionData) -> Vec<u8> {
    borsh::to_vec(data).unwrap()
}

pub fn initialize_program(payer: &Pubkey) -> Instruction {
    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new(*payer, true),
            AccountMeta::new(ProgramConfig::find_address().0, false),
            AccountMeta::new_readonly(system_program::ID, false),
        ],
        data: encode(&BuilderBondInstructionData::InitializeProgram),
    }
}

/// Set the program admin.
///
/// Two keys, because the processor reads two: `upgrade_authority` signs and is checked against the
/// program data account, and `admin_key` is the value written to the config. They are the same key
/// in these tests and need not be in production, so a single parameter would make the ordinary
/// case inexpressible.
pub fn set_admin(upgrade_authority: &Pubkey, admin_key: &Pubkey) -> Instruction {
    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new_readonly(get_program_data_address(&ID).0, false),
            AccountMeta::new_readonly(*upgrade_authority, true),
            AccountMeta::new(ProgramConfig::find_address().0, false),
        ],
        data: encode(&BuilderBondInstructionData::SetAdmin(*admin_key)),
    }
}

pub fn set_paused(admin: &Pubkey, paused: bool) -> Instruction {
    use crate::instruction::{ProgramConfiguration, ProgramFlagConfiguration};

    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new_readonly(*admin, true),
            AccountMeta::new(ProgramConfig::find_address().0, false),
        ],
        data: encode(&BuilderBondInstructionData::ConfigureProgram(
            ProgramConfiguration::Flag(ProgramFlagConfiguration::IsPaused(paused)),
        )),
    }
}

pub fn set_tier_parameters(
    admin: &Pubkey,
    up_to_1gbps_2z_amount: u64,
    up_to_5gbps_2z_amount: u64,
    unmetered_2z_amount: u64,
) -> Instruction {
    use crate::instruction::ProgramConfiguration;

    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new_readonly(*admin, true),
            AccountMeta::new(ProgramConfig::find_address().0, false),
        ],
        data: encode(&BuilderBondInstructionData::ConfigureProgram(
            ProgramConfiguration::TierParameters {
                up_to_1gbps_2z_amount,
                up_to_5gbps_2z_amount,
                unmetered_2z_amount,
            },
        )),
    }
}

pub fn initialize_builder_bond(
    builder: &Pubkey,
    bond_index: u64,
    committed_rate_bits_per_sec: u64,
) -> Instruction {
    initialize_builder_bond_with_mint(
        builder,
        bond_index,
        committed_rate_bits_per_sec,
        &DOUBLEZERO_MINT_KEY,
    )
}

/// [`initialize_builder_bond`] for a caller that picks the 2Z mint at run time. The crate's
/// `DOUBLEZERO_MINT_KEY` is fixed by the `development` feature at build time, so one client that
/// serves devnet and mainnet-beta names the mint itself.
pub fn initialize_builder_bond_with_mint(
    builder: &Pubkey,
    bond_index: u64,
    committed_rate_bits_per_sec: u64,
    mint: &Pubkey,
) -> Instruction {
    let bond_key = BuilderBond::find_address(builder, bond_index).0;

    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new_readonly(ProgramConfig::find_address().0, false),
            AccountMeta::new(*builder, true),
            AccountMeta::new(bond_key, false),
            AccountMeta::new(state::find_2z_token_pda_address(&bond_key).0, false),
            AccountMeta::new_readonly(*mint, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
            AccountMeta::new_readonly(system_program::ID, false),
        ],
        data: encode(&BuilderBondInstructionData::InitializeBuilderBond {
            bond_index,
            committed_rate_bits_per_sec,
        }),
    }
}

pub fn post_bond(
    builder: &Pubkey,
    bond_index: u64,
    source_token_account: &Pubkey,
    amount: u64,
) -> Instruction {
    let bond_key = BuilderBond::find_address(builder, bond_index).0;

    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new_readonly(ProgramConfig::find_address().0, false),
            AccountMeta::new_readonly(*builder, true),
            AccountMeta::new(bond_key, false),
            AccountMeta::new(state::find_2z_token_pda_address(&bond_key).0, false),
            AccountMeta::new(*source_token_account, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
        ],
        data: encode(&BuilderBondInstructionData::PostBond { amount }),
    }
}

pub fn withdraw(
    builder: &Pubkey,
    bond_index: u64,
    destination_token_account: &Pubkey,
    amount: u64,
) -> Instruction {
    let bond_key = BuilderBond::find_address(builder, bond_index).0;

    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new_readonly(ProgramConfig::find_address().0, false),
            AccountMeta::new_readonly(*builder, true),
            AccountMeta::new(bond_key, false),
            AccountMeta::new(state::find_2z_token_pda_address(&bond_key).0, false),
            AccountMeta::new(*destination_token_account, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
        ],
        data: encode(&BuilderBondInstructionData::Withdraw { amount }),
    }
}

/// Move a bond's hold expiry. Development builds only; the test binary is one.
pub fn set_hold_expiry(admin: &Pubkey, builder: &Pubkey, bond_index: u64, at: i64) -> Instruction {
    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new_readonly(ProgramConfig::find_address().0, false),
            AccountMeta::new_readonly(*admin, true),
            AccountMeta::new(BuilderBond::find_address(builder, bond_index).0, false),
        ],
        data: encode(&BuilderBondInstructionData::SetHoldExpiry {
            hold_expires_at: at,
        }),
    }
}
