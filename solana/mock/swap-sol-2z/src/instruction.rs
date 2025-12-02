use std::io;

use borsh::{BorshDeserialize, BorshSerialize};
use doublezero_program_tools::{
    instruction::try_build_instruction, zero_copy, Discriminator, DISCRIMINATOR_LEN,
};
use doublezero_revenue_distribution::instruction::account::WithdrawSolAccounts;
use solana_instruction::{AccountMeta, Instruction};
use solana_pubkey::Pubkey;
use solana_sysvar::rent::Rent;

use crate::{state::FillsRegistry, ID};

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum MockSwapSol2zInstructionData {
    InitializeFillsRegistry,
    BuySol {
        amount_2z_in: u64,
        amount_sol_out: u64,
    },
    DequeueFills(u64),
}

impl MockSwapSol2zInstructionData {
    pub const INITIALIZE_FILLS_TRACKER: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new([1, 0, 0, 0, 0, 0, 0, 0]);
    pub const BUY_SOL: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new([2, 0, 0, 0, 0, 0, 0, 0]);
    pub const DEQUEUE_FILLS: Discriminator<DISCRIMINATOR_LEN> =
        Discriminator::new([146, 69, 6, 12, 174, 95, 136, 61]);
}

impl BorshDeserialize for MockSwapSol2zInstructionData {
    fn deserialize_reader<R: io::Read>(reader: &mut R) -> std::io::Result<Self> {
        match Discriminator::deserialize_reader(reader)? {
            Self::INITIALIZE_FILLS_TRACKER => Ok(Self::InitializeFillsRegistry),
            Self::BUY_SOL => {
                let amount_2z_in = BorshDeserialize::deserialize_reader(reader)?;
                let amount_sol_out = BorshDeserialize::deserialize_reader(reader)?;
                Ok(Self::BuySol {
                    amount_2z_in,
                    amount_sol_out,
                })
            }
            Self::DEQUEUE_FILLS => {
                BorshDeserialize::deserialize_reader(reader).map(Self::DequeueFills)
            }
            _ => Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "Invalid discriminator",
            )),
        }
    }
}

impl BorshSerialize for MockSwapSol2zInstructionData {
    fn serialize<W: io::Write>(&self, writer: &mut W) -> io::Result<()> {
        match self {
            Self::InitializeFillsRegistry => Self::INITIALIZE_FILLS_TRACKER.serialize(writer),
            Self::BuySol {
                amount_2z_in,
                amount_sol_out,
            } => {
                Self::BUY_SOL.serialize(writer)?;
                amount_2z_in.serialize(writer)?;
                amount_sol_out.serialize(writer)
            }
            Self::DequeueFills(max_sol_amount) => {
                Self::DEQUEUE_FILLS.serialize(writer)?;
                max_sol_amount.serialize(writer)
            }
        }
    }
}

pub fn create_and_initialize_fills_tracker(
    payer_key: &Pubkey,
    new_fills_tracker_key: &Pubkey,
) -> (Instruction, Instruction) {
    let size = zero_copy::data_end::<FillsRegistry>();
    let rent_exemption_lamports = Rent::default().minimum_balance(size);

    let create_account_ix = solana_system_interface::instruction::create_account(
        payer_key,
        new_fills_tracker_key,
        rent_exemption_lamports,
        size as u64,
        &ID,
    );

    let initialize_fills_tracker_ix = try_build_instruction(
        &ID,
        vec![AccountMeta::new(*new_fills_tracker_key, false)],
        &MockSwapSol2zInstructionData::InitializeFillsRegistry,
    )
    .unwrap();

    (create_account_ix, initialize_fills_tracker_ix)
}

pub fn buy_sol(
    fills_tracker_key: &Pubkey,
    src_token_key: &Pubkey,
    transfer_authority_key: &Pubkey,
    sol_destination_key: &Pubkey,
    amount_2z_in: u64,
    amount_sol_out: u64,
) -> Instruction {
    let WithdrawSolAccounts {
        program_config_key: rd_program_config_key,
        withdraw_sol_authority_key,
        journal_key: rd_journal_key,
        sol_destination_key,
    } = WithdrawSolAccounts::new(&ID, sol_destination_key);

    let rd_swap_authority_key =
        doublezero_revenue_distribution::state::find_swap_authority_address().0;
    let dst_token_key =
        doublezero_revenue_distribution::state::find_2z_token_pda_address(&rd_swap_authority_key).0;

    try_build_instruction(
        &ID,
        vec![
            AccountMeta::new(*fills_tracker_key, false),
            AccountMeta::new(*src_token_key, false),
            AccountMeta::new(doublezero_revenue_distribution::DOUBLEZERO_MINT_KEY, false),
            AccountMeta::new(dst_token_key, false),
            AccountMeta::new(*transfer_authority_key, true),
            AccountMeta::new(rd_program_config_key, false),
            AccountMeta::new(withdraw_sol_authority_key, false),
            AccountMeta::new(rd_journal_key, false),
            AccountMeta::new(sol_destination_key, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
            AccountMeta::new_readonly(doublezero_revenue_distribution::ID, false),
        ],
        &MockSwapSol2zInstructionData::BuySol {
            amount_2z_in,
            amount_sol_out,
        },
    )
    .unwrap()
}
