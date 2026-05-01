use crate::{
    processors::check_foundation_allowlist,
    state::{
        geolocation_user::{GeolocationBillingConfig, GeolocationPaymentStatus},
        geolocation_user_view::GeolocationUserView,
    },
};
use borsh::{BorshDeserialize, BorshSerialize};
use solana_program::{
    account_info::{next_account_info, AccountInfo},
    entrypoint::ProgramResult,
    msg,
    program_error::ProgramError,
    pubkey::Pubkey,
};

#[derive(BorshSerialize, BorshDeserialize, Debug, PartialEq, Clone)]
pub struct UpdatePaymentStatusArgs {
    pub payment_status: GeolocationPaymentStatus,
    pub last_deduction_dz_epoch: Option<u64>,
}

pub fn process_update_payment_status(
    program_id: &Pubkey,
    accounts: &[AccountInfo],
    args: &UpdatePaymentStatusArgs,
) -> ProgramResult {
    let accounts_iter = &mut accounts.iter();

    let user_account = next_account_info(accounts_iter)?;
    let program_config_account = next_account_info(accounts_iter)?;
    let serviceability_globalstate_account = next_account_info(accounts_iter)?;
    let payer_account = next_account_info(accounts_iter)?;

    if !payer_account.is_signer {
        msg!("Payer must be a signer");
        return Err(ProgramError::MissingRequiredSignature);
    }

    check_foundation_allowlist(
        program_config_account,
        serviceability_globalstate_account,
        payer_account,
        program_id,
    )?;

    if user_account.owner != program_id {
        msg!("Invalid GeolocationUser Account Owner");
        return Err(ProgramError::IllegalOwner);
    }
    if !user_account.is_writable {
        msg!("GeolocationUser account must be writable");
        return Err(ProgramError::InvalidAccountData);
    }

    let mut view = GeolocationUserView::try_from_account(user_account)?;

    view.payment_status = args.payment_status;

    if let Some(epoch) = args.last_deduction_dz_epoch {
        match &mut view.billing {
            GeolocationBillingConfig::FlatPerEpoch(config) => {
                config.last_deduction_dz_epoch = epoch;
            }
        }
    }

    view.write_prefix(user_account)?;

    Ok(())
}
