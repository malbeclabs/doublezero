use solana_account_info::AccountInfo;
use solana_cpi::invoke_signed_unchecked;
use solana_program_error::ProgramResult;
use solana_pubkey::Pubkey;
use solana_system_interface::instruction as system_instruction;
use solana_sysvar::{rent::Rent, Sysvar};

use super::Invoker;

#[derive(Debug, Default)]
pub struct CreateAccountOptions<'a> {
    pub rent_sysvar: Option<&'a Rent>,
    pub additional_lamports: Option<u64>,
}

pub fn try_create_account(
    payer: Invoker,
    new_account: Invoker,
    current_lamports: u64,
    data_len: usize,
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    options: CreateAccountOptions,
) -> ProgramResult {
    let CreateAccountOptions {
        rent_sysvar,
        additional_lamports,
    } = options;

    let rent_exemption_lamports = match rent_sysvar {
        Some(rent_sysvar) => rent_sysvar.minimum_balance(data_len),
        None => Rent::get().unwrap().minimum_balance(data_len),
    };

    let lamports = additional_lamports
        .unwrap_or_default()
        .saturating_add(rent_exemption_lamports);

    if current_lamports == 0 {
        // PC Load Letter?
        match (payer, new_account) {
            (
                Invoker::Pda {
                    key: payer_key,
                    signer_seeds: payer_signer_seeds,
                },
                Invoker::Pda {
                    key: new_account_key,
                    signer_seeds: new_account_signer_seeds,
                },
            ) => {
                let create_account_ix = system_instruction::create_account(
                    payer_key,
                    new_account_key,
                    lamports,
                    data_len as u64,
                    program_id,
                );
                invoke_signed_unchecked(
                    &create_account_ix,
                    accounts,
                    &[payer_signer_seeds, new_account_signer_seeds],
                )?;
            }
            (
                Invoker::Pda {
                    key: payer_key,
                    signer_seeds: payer_signer_seeds,
                },
                Invoker::Signer(new_account_key),
            ) => {
                let create_account_ix = system_instruction::create_account(
                    payer_key,
                    new_account_key,
                    lamports,
                    data_len as u64,
                    program_id,
                );
                invoke_signed_unchecked(&create_account_ix, accounts, &[payer_signer_seeds])?;
            }
            (
                Invoker::Signer(payer_key),
                Invoker::Pda {
                    key: new_account_key,
                    signer_seeds: new_account_signer_seeds,
                },
            ) => {
                let create_account_ix = system_instruction::create_account(
                    payer_key,
                    new_account_key,
                    lamports,
                    data_len as u64,
                    program_id,
                );
                invoke_signed_unchecked(&create_account_ix, accounts, &[new_account_signer_seeds])?;
            }
            (Invoker::Signer(payer_key), Invoker::Signer(new_account_key)) => {
                let create_account_ix = system_instruction::create_account(
                    payer_key,
                    new_account_key,
                    lamports,
                    data_len as u64,
                    program_id,
                );
                invoke_signed_unchecked(&create_account_ix, accounts, &[])?;
            }
        }
    } else {
        let new_account_key = match new_account {
            Invoker::Pda {
                key: new_account_key,
                signer_seeds: new_account_signer_seeds,
            } => {
                let allocate_ix = system_instruction::allocate(new_account_key, data_len as u64);
                invoke_signed_unchecked(&allocate_ix, accounts, &[new_account_signer_seeds])?;

                let assign_ix = system_instruction::assign(new_account_key, program_id);
                invoke_signed_unchecked(&assign_ix, accounts, &[new_account_signer_seeds])?;

                new_account_key
            }
            Invoker::Signer(new_account_key) => {
                let allocate_ix = system_instruction::allocate(new_account_key, data_len as u64);
                invoke_signed_unchecked(&allocate_ix, accounts, &[])?;

                let assign_ix = system_instruction::assign(new_account_key, program_id);
                invoke_signed_unchecked(&assign_ix, accounts, &[])?;

                new_account_key
            }
        };

        let lamport_diff = lamports.saturating_sub(current_lamports);

        // Transfer as much as we need for this account to be rent-exempt.
        if lamport_diff != 0 {
            match payer {
                Invoker::Pda {
                    key: payer_key,
                    signer_seeds: payer_signer_seeds,
                } => {
                    let transfer_ix =
                        system_instruction::transfer(payer_key, new_account_key, lamport_diff);
                    invoke_signed_unchecked(&transfer_ix, accounts, &[payer_signer_seeds])?;
                }
                Invoker::Signer(payer_key) => {
                    let transfer_ix =
                        system_instruction::transfer(payer_key, new_account_key, lamport_diff);
                    invoke_signed_unchecked(&transfer_ix, accounts, &[])?;
                }
            }
        }
    }

    Ok(())
}
