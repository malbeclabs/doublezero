//! Narrow ledger-client trait covering the subset of `CliCommand` methods used
//! by daemon verbs.
//!
//! The binary provides a blanket adapter from `CliCommandImpl` → `LedgerClient`.
//! This trait is intentionally narrow — it only includes SDK operations that
//! daemon-control verbs actually call. It will be expanded as verbs migrate
//! into this crate.

use std::collections::HashMap;

use doublezero_config::Environment;
use doublezero_sdk::{Device, GlobalState, User};
use mockall::automock;
use solana_sdk::pubkey::Pubkey;

/// The subset of SDK/ledger operations used by daemon-control verbs.
///
/// All daemon verbs need `get_environment()` for the daemon/client environment
/// match check. More complex verbs (`connect`, `disconnect`) use the
/// additional methods — those are added as those verbs migrate into this crate.
#[automock]
pub trait LedgerClient: Send + Sync {
    fn get_environment(&self) -> Environment;

    /// The operator's payer pubkey (used to distinguish self-owned users).
    fn get_payer(&self) -> Pubkey;

    /// Verify the operator has a usable keypair (id.json or an alternative
    /// keypair source) and a non-zero credit balance — the preconditions for
    /// mutating ledger operations. Diagnostics route through `tracing`.
    fn check_requirements(&self) -> eyre::Result<()>;

    /// Fetch onchain global state (its `feed_authority_pk` identifies
    /// shred-oracle-managed users during teardown).
    fn get_globalstate(&self) -> eyre::Result<GlobalState>;

    /// List all users on the ledger.
    fn list_user(&self) -> eyre::Result<HashMap<Pubkey, User>>;

    /// Delete the user account at `pubkey`.
    fn delete_user(&self, pubkey: Pubkey) -> eyre::Result<()>;

    /// Fetch the user account at `pubkey` (used to poll for deletion).
    fn get_user(&self, pubkey: Pubkey) -> eyre::Result<User>;

    /// List all devices known to the ledger, keyed by pubkey. Used by
    /// `latency` (and, once migrated, `connect`) to map latency records to
    /// onchain device state.
    fn list_device(&self) -> eyre::Result<HashMap<Pubkey, Device>>;
}
