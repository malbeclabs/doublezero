use borsh::BorshSerialize;
use doublezero_program_common::create_account::try_create_account;
use solana_program::{
    account_info::AccountInfo,
    entrypoint::ProgramResult,
    program::invoke_signed,
    pubkey::Pubkey,
    system_instruction,
    sysvar::{rent::Rent, Sysvar},
};

pub trait AccountSize {
    fn size(&self) -> usize;
}
pub trait AccountSeed {
    fn seed(&self, seed: &mut Vec<u8>);
}

pub fn write_account<'a, D: BorshSerialize + AccountSize + AccountSeed>(
    account: &AccountInfo<'a>,
    data: &D,
    program_id: &Pubkey,
    payer: &AccountInfo<'a>,
    system_program: &AccountInfo<'a>,
) -> ProgramResult {
    // Size of our index account
    let required_space = data.size();

    // Calculate minimum balance for rent exemption
    let rent = Rent::get()?;
    let required_lamports = rent.minimum_balance(required_space);

    let mut seed: Vec<u8> = Vec::new();
    data.seed(&mut seed);

    if account.try_borrow_data()?.is_empty() {
        try_create_account(
            payer.key,
            account.key,
            account.lamports(),
            required_space,
            program_id,
            &[account.clone(), payer.clone(), system_program.clone()],
            &[seed.as_slice()],
        )?;
    } else {
        // If the account is already initialized, we need to check if it has enough space
        if account.data_len() != required_space {
            account.realloc(required_space, false)?;

            // If the account is not large enough, we need to transfer more lamports
            if required_space > account.data_len() {
                let payment = required_lamports - account.lamports();

                invoke_signed(
                    &system_instruction::transfer(payer.key, account.key, payment),
                    &[account.clone(), payer.clone(), system_program.clone()],
                    &[&[seed.as_slice()]],
                )?;
            }
        }
    }

    let mut account_data = &mut account.data.borrow_mut()[..];
    data.serialize(&mut account_data).unwrap();

    Ok(())
}
