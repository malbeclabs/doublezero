//! Global allocator that uses the heap frame the transaction requested.
//!
//! The stock `entrypoint!` allocator is fixed at `HEAP_LENGTH` (32 KiB) and bumps *downward* from
//! `HEAP_START_ADDRESS + 32 KiB`, so a `ComputeBudgetInstruction::request_heap_frame` in the
//! transaction is never used: every instruction runs on 32 KiB however much heap it paid for.
//!
//! This allocator bumps *upward* from `HEAP_START_ADDRESS`, bounded by the protocol-max heap frame
//! (256 KiB). Growing from the bottom is what makes the bound safe to set above the default: an
//! instruction only touches as much heap as it actually uses, so a transaction without a heap-frame
//! request keeps working as long as it stays within the first 32 KiB, and one that requested more
//! can use all of it. The program cannot query the size of the frame the runtime mapped, so an
//! allocation that runs past it returns a pointer into unmapped memory and the first write to it
//! aborts the instruction with an access violation, where the stock allocator would have returned
//! null and aborted with an out-of-memory error. Either way the transaction fails.
//!
//! Otherwise this is the stock `solana_program_entrypoint::BumpAllocator`: the cursor lives in the
//! first word of the heap, which the runtime zero-fills, so `0` means "nothing allocated yet", and
//! nothing is ever freed. The one addition is that `realloc` of the most recent allocation resizes
//! it in place, so a `Vec` that keeps growing (such as an account serialization buffer) does not
//! leave a stale copy behind on every doubling.

use core::{
    alloc::{GlobalAlloc, Layout},
    mem::size_of,
    ptr::{copy_nonoverlapping, null_mut},
};

/// Largest heap frame a transaction can request (`MAX_HEAP_FRAME_BYTES` in the runtime's compute
/// budget).
pub const MAX_HEAP_FRAME_BYTES: usize = 256 * 1024;

const CURSOR_SIZE: usize = size_of::<usize>();

/// Bump allocator over `[start, start + len)` that grows upward. The first word of the region
/// holds the address of the next free byte.
pub struct UpwardBumpAllocator {
    start: usize,
    len: usize,
}

impl UpwardBumpAllocator {
    /// # Safety
    ///
    /// `start` must be aligned for `usize`, `start + len` must not overflow, the first
    /// `size_of::<usize>()` bytes of the region must be writable and zero before the first
    /// allocation, and nothing else may use the region while the allocator or any of its
    /// allocations are alive. Memory past what the runtime actually mapped is handed out but faults
    /// when touched (see the module docs).
    pub const unsafe fn new(start: usize, len: usize) -> Self {
        Self { start, len }
    }

    /// The first word of the region: the address of the next free byte, or `0` (the runtime
    /// zero-fills the heap) before the first allocation.
    #[inline(always)]
    fn cursor_ptr(&self) -> *mut usize {
        self.start as *mut usize
    }
}

unsafe impl GlobalAlloc for UpwardBumpAllocator {
    #[inline]
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        let mut pos = *self.cursor_ptr();
        if pos == 0 {
            pos = self.start + CURSOR_SIZE;
        }
        // Round up to the alignment, a nonzero power of two. Neither addition can overflow:
        // `Layout` keeps `size` rounded up to `align` within `isize::MAX`, and `pos` is a heap
        // address far below 2^62.
        let mask = layout.align() - 1;
        let addr = (pos + mask) & !mask;
        let end = addr + layout.size();
        if end > self.start + self.len {
            return null_mut();
        }
        *self.cursor_ptr() = end;
        addr as *mut u8
    }

    #[inline]
    unsafe fn dealloc(&self, _: *mut u8, _: Layout) {
        // Never frees, like the stock allocator. Keeping this empty lets every drop site compile
        // away; reclaiming even just the last allocation here measurably cost both program size
        // and compute.
    }

    #[inline]
    unsafe fn realloc(&self, ptr: *mut u8, layout: Layout, new_size: usize) -> *mut u8 {
        // The most recent allocation grows or shrinks in place: `ptr` is already aligned for
        // `layout`, whose alignment `realloc` keeps.
        if ptr as usize + layout.size() == *self.cursor_ptr() {
            let end = ptr as usize + new_size;
            if end > self.start + self.len {
                return null_mut();
            }
            *self.cursor_ptr() = end;
            return ptr;
        }
        // Anything else moves, as with the default `realloc`.
        let new_ptr = self.alloc(Layout::from_size_align_unchecked(new_size, layout.align()));
        if !new_ptr.is_null() {
            copy_nonoverlapping(ptr, new_ptr, layout.size().min(new_size));
        }
        new_ptr
    }
}

/// Installed only in the deployed program: native test builds use the host allocator, and a crate
/// depending on this one with `no-entrypoint` brings its own.
#[cfg(all(target_os = "solana", not(feature = "no-entrypoint")))]
#[global_allocator]
static ALLOCATOR: UpwardBumpAllocator = unsafe {
    UpwardBumpAllocator::new(
        solana_program::entrypoint::HEAP_START_ADDRESS as usize,
        MAX_HEAP_FRAME_BYTES,
    )
};

#[cfg(test)]
mod tests {
    use super::*;

    /// A zeroed, `usize`-aligned arena standing in for the SBF heap region.
    fn arena(words: usize) -> (Vec<u64>, UpwardBumpAllocator) {
        let mut buf = vec![0u64; words];
        let alloc = unsafe { UpwardBumpAllocator::new(buf.as_mut_ptr() as usize, words * 8) };
        (buf, alloc)
    }

    #[test]
    fn allocates_upward_from_after_the_cursor_word() {
        let (buf, a) = arena(16);
        let start = buf.as_ptr() as usize;
        unsafe {
            let p1 = a.alloc(Layout::from_size_align(3, 1).unwrap());
            assert_eq!(p1 as usize, start + CURSOR_SIZE);
            let p2 = a.alloc(Layout::from_size_align(8, 8).unwrap());
            assert_eq!(p2 as usize, start + 2 * CURSOR_SIZE);
            assert_eq!(p2 as usize % 8, 0);
        }
    }

    #[test]
    fn returns_null_past_the_end() {
        let (_buf, a) = arena(4);
        unsafe {
            assert!(!a.alloc(Layout::from_size_align(24, 8).unwrap()).is_null());
            assert!(a.alloc(Layout::from_size_align(1, 1).unwrap()).is_null());
            assert!(a
                .alloc(Layout::from_size_align(usize::MAX / 2, 1).unwrap())
                .is_null());
        }
    }

    #[test]
    fn last_allocation_is_resized_in_place() {
        let (_buf, a) = arena(16);
        let l8 = Layout::from_size_align(8, 8).unwrap();
        unsafe {
            let p1 = a.alloc(l8);
            let grown = a.realloc(p1, l8, 32);
            assert_eq!(grown, p1);
            *grown.add(31) = 7;

            let p2 = a.alloc(l8);
            assert_eq!(p2 as usize, p1 as usize + 32);
            // Nothing is freed, not even the last allocation.
            a.dealloc(p2, l8);
            assert_eq!(a.alloc(l8) as usize, p2 as usize + 8);

            // `p1` is no longer last: realloc moves it and keeps its contents.
            let moved = a.realloc(grown, Layout::from_size_align(32, 8).unwrap(), 40);
            assert_eq!(moved as usize, p2 as usize + 16);
            assert_eq!(*moved.add(31), 7);
        }
    }

    #[test]
    fn in_place_growth_is_bounded() {
        let (_buf, a) = arena(4);
        let l8 = Layout::from_size_align(8, 8).unwrap();
        unsafe {
            let p = a.alloc(l8);
            assert!(a.realloc(p, l8, 25).is_null());
            assert_eq!(a.realloc(p, l8, 24), p);
        }
    }
}
