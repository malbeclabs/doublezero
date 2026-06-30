//! Module-crate-owned subcommand enums per RFC-20 (§Module contract item 2).
//!
//! Each file in this module defines one resource's clap `Subcommand` enum
//! (`DeviceCommands`, `LinkCommands`, ...) wrapping the per-verb args types
//! that live next to the verbs themselves (`crate::device::create::*`,
//! `crate::link::create::*`, ...). A future PR adds a top-level
//! `ServiceabilityCommand` aggregator and hoists these variants into the
//! unified `doublezero` binary via `#[command(flatten)]`.

pub mod accesspass;
pub mod command;
pub mod config;

pub use command::ServiceabilityCommand;
pub mod contributor;
pub mod device;
pub mod exchange;
pub mod feed;
pub mod globalconfig;
pub mod link;
pub mod location;
pub mod migrate;
pub mod multicastgroup;
pub mod permission;
pub mod resource;
pub mod tenant;
pub mod user;
