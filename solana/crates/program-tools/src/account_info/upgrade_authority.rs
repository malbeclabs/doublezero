use solana_account_info::AccountInfo;
use solana_loader_v3_interface::state::UpgradeableLoaderState;
use solana_msg::msg;
use solana_program_error::ProgramError;
use solana_pubkey::Pubkey;

use crate::get_program_data_address;

use super::{
    try_next_enumerated_account, EnumeratedAccountInfoIter, NextAccountOptions, TryNextAccounts,
};

pub struct UpgradeAuthority<'a, 'b> {
    pub program_data: (usize, &'a AccountInfo<'b>),
    pub owner: (usize, &'a AccountInfo<'b>),
}

impl<'a, 'b> TryNextAccounts<'a, 'b, &'a Pubkey> for UpgradeAuthority<'a, 'b> {
    fn try_next_accounts(
        accounts_iter: &mut EnumeratedAccountInfoIter<'a, 'b>,
        program_id: &'a Pubkey,
    ) -> Result<Self, ProgramError> {
        // Index == 0.
        let (index, program_data_info) =
            try_next_enumerated_account(accounts_iter, Default::default())?;
        if program_data_info.key != &get_program_data_address(program_id).0 {
            msg!("Invalid program data address (account {})", index);
            return Err(ProgramError::InvalidAccountData);
        }

        // Index == 1.
        let (index, owner_info) = try_next_enumerated_account(
            accounts_iter,
            NextAccountOptions {
                must_be_signer: true,
                ..Default::default()
            },
        )?;

        if !owner_info.is_signer {
            msg!("Owner (account {}) must be signer", index);
            return Err(ProgramError::MissingRequiredSignature);
        }

        let program_data_info_data = program_data_info.data.borrow();
        match bincode::deserialize(&program_data_info_data) {
            Ok(UpgradeableLoaderState::ProgramData {
                slot: _,
                upgrade_authority_address: Some(authority),
            }) => {
                if owner_info.key != &authority {
                    msg!(
                        "Owner (account {}) must match upgrade authority from program data (account {})", index, index - 1
                    );
                    Err(ProgramError::InvalidAccountData)
                } else {
                    Ok(Self {
                        program_data: (index - 1, program_data_info),
                        owner: (index, owner_info),
                    })
                }
            }
            _ => {
                msg!("Invalid program data (account {})", index - 1);
                Err(ProgramError::InvalidAccountData)
            }
        }
    }
}

// TODO: Add unit tests.
