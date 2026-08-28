mod iter;
mod upgrade_authority;

pub use iter::*;
pub use upgrade_authority::*;

//

use std::cell::{Ref, RefMut};

use solana_account_info::AccountInfo;
use solana_program_error::ProgramError;

/// A silly (but effective) way to make sure we get Ref<[u8]> because
/// [AccountInfo::try_borrow_data] returns Ref<&mut [u8]>.
#[inline(always)]
pub fn try_borrow_data<'a>(
    account_info: &'a AccountInfo<'_>,
) -> Result<Ref<'a, [u8]>, ProgramError> {
    let data = account_info.try_borrow_data()?;
    Ok(Ref::map(data, |data| &data[..]))
}

/// A silly (but effective) way to make sure we get RefMut<[u8]> because
/// [AccountInfo::try_borrow_mut_data] returns RefMut<&mut [u8]>.
#[inline(always)]
pub fn try_borrow_mut_data<'a>(
    account_info: &'a AccountInfo<'_>,
) -> Result<RefMut<'a, [u8]>, ProgramError> {
    let data = account_info.try_borrow_mut_data()?;
    Ok(RefMut::map(data, |data| &mut data[..]))
}
