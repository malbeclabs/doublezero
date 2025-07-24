pub mod account_info;
pub mod instruction;
pub mod recipe;
pub mod zero_copy;
//

use std::fmt::Display;

use borsh::{BorshDeserialize, BorshSerialize};

/// If there is a discriminator used for any data, it should be 8 bytes long. For account data
/// represented as a C-struct, 8 bytes is a convenient size for the discriminator.
///
/// NOTE: Some programs may have instruction selectors that do not follow this rule (where there is
/// only one byte to discriminate among instructions).
pub const DISCRIMINATOR_LEN: usize = 8;

pub trait PrecomputedDiscriminator {
    const DISCRIMINATOR: Discriminator<8>;

    #[inline(always)]
    fn has_discriminator(data: &[u8]) -> bool {
        data.len() >= DISCRIMINATOR_LEN && data[..DISCRIMINATOR_LEN] == Self::DISCRIMINATOR.0
    }

    fn discriminator_slice() -> &'static [u8] {
        &Self::DISCRIMINATOR.0
    }
}

#[derive(Debug, BorshDeserialize, BorshSerialize, Clone, Copy, PartialEq, Eq)]
pub struct Discriminator<const N: usize>([u8; N]);

impl<const N: usize> Display for Discriminator<N> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        for byte in &self.0 {
            write!(f, "{:02x}", byte)?;
        }
        Ok(())
    }
}

impl<const N: usize> Discriminator<N> {
    pub const fn new_sha2(input: &[u8]) -> Self {
        assert!(N <= 32, "Exceeds 32 bytes");

        let digest = sha2_const_stable::Sha256::new().update(input).finalize();
        let mut trimmed = [0; N];
        let mut i = 0;

        loop {
            if i >= N {
                break;
            }
            trimmed[i] = digest[i];
            i += 1;
        }

        Self(trimmed)
    }
}

#[cfg(test)]
mod test {
    use super::*;

    #[test]
    fn test_sha2_discriminator() {
        assert_eq!(
            Discriminator::new_sha2(b"hello world"),
            Discriminator([
                185, 77, 39, 185, 147, 77, 62, 8, 165, 46, 82, 215, 218, 125, 171, 250, 196, 132,
                239, 227, 122, 83, 128, 238, 144, 136, 247, 172, 226, 239, 205
            ])
        );
    }
}
