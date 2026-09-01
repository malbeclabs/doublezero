pub mod record;
pub mod zero_copy;

//

use solana_sdk::{account::Account, rent::Rent};

pub fn balance(account: &Account, rent: &Rent) -> u64 {
    let rent_exemption_lamports = rent.minimum_balance(account.data.len());
    account.lamports.saturating_sub(rent_exemption_lamports)
}
