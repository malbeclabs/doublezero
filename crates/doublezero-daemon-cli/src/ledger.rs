//! Narrow ledger-client trait covering the subset of `CliCommand` methods used
//! by daemon verbs.
//!
//! The binary provides a blanket adapter from `CliCommandImpl` → `LedgerClient`.
//! This trait is intentionally narrow — it only includes SDK operations that
//! daemon-control verbs actually call. It will be expanded as verbs migrate
//! into this crate.

use doublezero_config::Environment;
use mockall::automock;

/// The subset of SDK/ledger operations used by daemon-control verbs.
///
/// All daemon verbs need `get_environment()` for the daemon/client environment
/// match check. More complex verbs (`connect`, `disconnect`) use the
/// additional methods — those will be added as those verbs migrate into this
/// crate.
#[automock]
pub trait LedgerClient: Send + Sync {
    fn get_environment(&self) -> Environment;
}
