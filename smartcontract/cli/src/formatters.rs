//! Re-export of the shared display formatters.
//!
//! Implementations live in `doublezero-cli-core::formatters`. This module
//! preserves the existing import path so the serviceability crate's call
//! sites continue to compile unchanged during RFC-20 migration.

pub use doublezero_cli_core::formatters::{stringify_vec, DisplayVec};
