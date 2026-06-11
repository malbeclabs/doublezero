//! RFC-20 module crate for the `doublezero geolocation` subcommand tree.
//!
//! See `rfcs/rfc20-cli-standardization.md` and `docs/cli-standard.md`.

pub mod cli;
pub mod client;
pub mod init;
pub mod probe;
pub mod user;

pub use cli::{GeolocationArgs, GeolocationCommand};
pub use client::{GeoCliCommand, GeoCliCommandImpl};
