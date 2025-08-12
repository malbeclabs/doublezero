#[cfg(feature = "entrypoint")]
mod account_info;

#[cfg(feature = "entrypoint")]
pub use account_info::*;

//

use bytemuck::Pod;

use crate::{PrecomputedDiscriminator, DISCRIMINATOR_LEN};

pub const fn data_end<T: Pod + PrecomputedDiscriminator>() -> usize {
    DISCRIMINATOR_LEN + size_of::<T>()
}

pub const fn data_range<T: Pod + PrecomputedDiscriminator>() -> std::ops::Range<usize> {
    DISCRIMINATOR_LEN..data_end::<T>()
}

pub fn checked_from_bytes_with_discriminator<T>(data: &[u8]) -> Option<(&T, &[u8])>
where
    T: Pod + PrecomputedDiscriminator,
{
    let range = data_range::<T>();
    let (account_data, remaining_data) = data.split_at_checked(range.end)?;

    if T::has_discriminator(account_data) {
        Some((bytemuck::from_bytes(&account_data[range]), remaining_data))
    } else {
        None
    }
}
