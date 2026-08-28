//! RFC-20 module crate for the `doublezero passport` subcommand tree.
//!
//! See `rfcs/rfc20-cli-standardization.md`. This crate is library-only: it
//! exports a [`Command`] enum that derives clap's `Subcommand` and exposes an
//! async `execute(self, ctx: &CliContext, out: &mut impl Write)` on each verb.
//! All environment-derived configuration is read from [`CliContext`]; the crate
//! never reads environment variables, config files, or `argv` directly, and all
//! output is routed through the supplied writer.
//!
//! Both the unified `doublezero` binary and the offchain `doublezero-solana`
//! binary mount the same [`Command`] enum and supply their own `CliContext`.

mod access_validation;
pub mod command;
pub mod error;
pub mod fetch;
pub mod find_validator;
mod output;
pub mod prepare_access;
pub mod request_access;
mod shared;
mod util;

pub use command::Command;
pub use error::{PassportCliError, Result};
pub use shared::SharedAccessArgs;
