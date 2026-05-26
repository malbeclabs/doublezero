//! Shared utilities for DoubleZero CLI modules.
//!
//! Defined by RFC-20 (`rfcs/rfc20-cli-standardization.md`). This crate is the
//! small, dependency-light layer that every `doublezero-<module>-cli` module
//! crate reuses: a resolved configuration value (`CliContext`), preflight
//! bitflags, the shared input validators, the shared display formatters, and
//! the diagnostic-logging facade. Module crates own their typed backend
//! clients; this crate has no opinion about transports.

pub mod context;
pub mod error;
pub mod formatters;
pub mod logging;
pub mod requirements;
pub mod testing;
pub mod validators;

pub use context::{CliContext, CliContextBuilder, OutputFormat};
pub use error::{render_error, render_eyre, CliError, Result};
pub use logging::init_logging;
pub use requirements::RequirementCheck;
