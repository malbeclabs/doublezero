use solana_account_info::AccountInfo;
use solana_cpi::invoke_signed_unchecked;
use solana_program_error::ProgramResult;
use solana_pubkey::Pubkey;
use solana_sysvar::rent::Rent;
use spl_token::solana_program::program_pack::Pack;

use super::Invoker;

#[allow(clippy::too_many_arguments)]
pub fn try_create_token_account(
    payer: Invoker,
    new_token_account: Invoker,
    mint_key: &Pubkey,
    token_owner_key: &Pubkey,
    current_lamports: u64,
    accounts: &[AccountInfo],
    rent_sysvar: Option<&Rent>,
) -> ProgramResult {
    super::create_account::try_create_account(
        payer,
        new_token_account,
        current_lamports,
        spl_token::state::Account::LEN,
        &spl_token::ID,
        accounts,
        super::create_account::CreateAccountOptions {
            rent_sysvar,
            additional_lamports: None, // No additional lamports for token accounts
        },
    )?;

    let initialize_token_account_ix = spl_token::instruction::initialize_account3(
        &spl_token::ID,
        new_token_account.key(),
        mint_key,
        token_owner_key,
    )
    .unwrap();

    invoke_signed_unchecked(&initialize_token_account_ix, accounts, &[])?;

    Ok(())
}
