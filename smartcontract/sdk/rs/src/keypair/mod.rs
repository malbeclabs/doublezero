//! Keypair loading module with support for multiple input sources.
//!
//! This module provides flexible keypair loading with the following precedence:
//! 1. CLI argument (`--keypair /path/to/key.json`)
//! 2. Environment variable (`DOUBLEZERO_KEYPAIR` - can be JSON or file path)
//! 3. Stdin (if piped, not a TTY)
//! 4. Config file `keypair_path`
//! 5. Default path (`~/.config/doublezero/id.json`)
//!
//! # Example
//!
//! ```ignore
//! use doublezero_sdk::keypair::{load_keypair, KeypairSource, ENV_KEYPAIR};
//! use std::path::PathBuf;
//!
//! let result = load_keypair(
//!     Some(PathBuf::from("/path/from/cli")),
//!     Some(PathBuf::from("/path/from/config")),
//!     PathBuf::from("~/.config/doublezero/id.json"),
//! );
//!
//! match result {
//!     Ok(result) => {
//!         println!("Loaded keypair from: {}", result.source);
//!     }
//!     Err(e) => eprintln!("Failed to load keypair: {}", e),
//! }
//! ```
//!
//! # Environment Variable
//!
//! The `DOUBLEZERO_KEYPAIR` environment variable can contain either:
//! - A file path: `export DOUBLEZERO_KEYPAIR=/path/to/key.json`
//! - Raw JSON: `export DOUBLEZERO_KEYPAIR='[1,2,3,...,64 bytes]'`
//!
//! The loader auto-detects which format is used.

mod error;
mod loader;
mod source;

pub use error::KeypairLoadError;
pub use loader::{
    is_keypair_json_content, load_keypair, parse_keypair_json, KeypairLoadResult, ENV_KEYPAIR,
};
pub use source::KeypairSource;
