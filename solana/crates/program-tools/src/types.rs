use bitmaps::Bitmap;
use bytemuck::{Pod, Zeroable};

pub type Flags = u64;
pub type FlagsBitmap = Bitmap<{ Flags::BITS as usize }>;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(C)]
pub struct StorageGap<const N: usize>([[u8; 32]; N]);

impl<const N: usize> Default for StorageGap<N> {
    fn default() -> Self {
        Self([Default::default(); N])
    }
}

macro_rules! impl_storage_gap_pod_zeroable {
    ($($n:literal),* $(,)?) => {
        $(
            unsafe impl Zeroable for StorageGap<$n> {}
            unsafe impl Pod for StorageGap<$n> {}
        )*
    };
}

impl_storage_gap_pod_zeroable!(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16);
