//! Keypair loading module with support for multiple input sources.
//!
//! This module provides flexible keypair loading with the following precedence:
//! 1. CLI argument (`--keypair /path/to/key.json`)
//! 2. Stdin (if piped, not a TTY)
//! 3. Default path (`~/.config/solana/id.json`)
//!
//! # Example
//!
//! ```ignore
//! use solana_client_tools::keypair::try_load_keypair;
//! use std::path::PathBuf;
//!
//! // Load from CLI path, falling back to stdin or ~/.config/solana/id.json
//! let keypair = try_load_keypair(Some(PathBuf::from("/path/from/cli")))?;
//!
//! // Or let it use the default precedence chain
//! let keypair = try_load_keypair(None)?;
//! ```

mod error;
mod loader;
mod source;

pub use error::KeypairLoadError;
pub use loader::{KeypairLoadResult, load_keypair, parse_keypair_json, try_load_keypair};
pub use source::KeypairSource;
