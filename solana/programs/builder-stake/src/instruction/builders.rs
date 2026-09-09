//! Instruction builders for `builder-stake`.
//!
//! Every builder here fixes the account order the processor expects, so a caller cannot get it
//! wrong and a change to the processor has one place to update rather than one per caller.

use doublezero_program_tools::get_program_data_address;
use solana_instruction::{AccountMeta, Instruction};
use solana_pubkey::Pubkey;
use solana_system_interface::program as system_program;

use crate::{
    instruction::BuilderStakeInstructionData,
    state::{self, BuilderStake, ProgramConfig},
    DOUBLEZERO_MINT_KEY, ID,
};

fn encode(data: &BuilderStakeInstructionData) -> Vec<u8> {
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
        data: encode(&BuilderStakeInstructionData::InitializeProgram),
    }
}

pub fn set_admin(admin: &Pubkey) -> Instruction {
    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new_readonly(get_program_data_address(&ID).0, false),
            AccountMeta::new_readonly(*admin, true),
            AccountMeta::new(ProgramConfig::find_address().0, false),
        ],
        data: encode(&BuilderStakeInstructionData::SetAdmin(*admin)),
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
        data: encode(&BuilderStakeInstructionData::ConfigureProgram(
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
        data: encode(&BuilderStakeInstructionData::ConfigureProgram(
            ProgramConfiguration::TierParameters {
                up_to_1gbps_2z_amount,
                up_to_5gbps_2z_amount,
                unmetered_2z_amount,
            },
        )),
    }
}

pub fn initialize_builder_stake(
    builder: &Pubkey,
    stake_index: u64,
    committed_rate_bits_per_sec: u64,
) -> Instruction {
    let stake_key = BuilderStake::find_address(builder, stake_index).0;

    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new_readonly(ProgramConfig::find_address().0, false),
            AccountMeta::new(*builder, true),
            AccountMeta::new(stake_key, false),
            AccountMeta::new(state::find_2z_token_pda_address(&stake_key).0, false),
            AccountMeta::new_readonly(DOUBLEZERO_MINT_KEY, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
            AccountMeta::new_readonly(system_program::ID, false),
        ],
        data: encode(&BuilderStakeInstructionData::InitializeBuilderStake {
            stake_index,
            committed_rate_bits_per_sec,
        }),
    }
}

pub fn post_bond(
    builder: &Pubkey,
    stake_index: u64,
    source_token_account: &Pubkey,
    amount: u64,
) -> Instruction {
    let stake_key = BuilderStake::find_address(builder, stake_index).0;

    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new_readonly(ProgramConfig::find_address().0, false),
            AccountMeta::new_readonly(*builder, true),
            AccountMeta::new(stake_key, false),
            AccountMeta::new(state::find_2z_token_pda_address(&stake_key).0, false),
            AccountMeta::new(*source_token_account, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
        ],
        data: encode(&BuilderStakeInstructionData::PostBond { amount }),
    }
}

pub fn withdraw(
    builder: &Pubkey,
    stake_index: u64,
    destination_token_account: &Pubkey,
    amount: u64,
) -> Instruction {
    let stake_key = BuilderStake::find_address(builder, stake_index).0;

    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new_readonly(ProgramConfig::find_address().0, false),
            AccountMeta::new_readonly(*builder, true),
            AccountMeta::new(stake_key, false),
            AccountMeta::new(state::find_2z_token_pda_address(&stake_key).0, false),
            AccountMeta::new(*destination_token_account, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
        ],
        data: encode(&BuilderStakeInstructionData::Withdraw { amount }),
    }
}

/// Move a stake's hold expiry. Development builds only; the test binary is one.
pub fn set_hold_expiry(admin: &Pubkey, builder: &Pubkey, stake_index: u64, at: i64) -> Instruction {
    Instruction {
        program_id: ID,
        accounts: vec![
            AccountMeta::new_readonly(ProgramConfig::find_address().0, false),
            AccountMeta::new_readonly(*admin, true),
            AccountMeta::new(BuilderStake::find_address(builder, stake_index).0, false),
        ],
        data: encode(&BuilderStakeInstructionData::SetHoldExpiry {
            hold_expires_at: at,
        }),
    }
}
