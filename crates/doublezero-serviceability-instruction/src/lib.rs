//! Pure, RPC-free instruction builders for the `doublezero-serviceability`
//! program (RFC-26).
//!
//! Each builder is a pure function `build_xxx(...) -> Instruction` that takes
//! already-resolved arguments and returns a single unsigned
//! [`solana_program::instruction::Instruction`], with no network I/O — the
//! classic SPL pattern (`spl-token::instruction::transfer(...)`). The caller
//! composes the `Transaction`, prepends the [`compute_budget_prelude`], adds the
//! blockhash, and signs.
//!
//! Chain-derived values (globalstate `account_index`, `dz_prefix` counts,
//! RPC-read owner pubkeys) are passed in as explicit parameters; offline-derivable
//! PDAs are derived inside the builder via `doublezero_serviceability::pda`.
//!
//! # Excluded variants
//!
//! The `DoubleZeroInstruction` enum has 116 variants (tags 0–115, contiguous).
//! Builders cover only the buildable variants; the following have **no builder**
//! (no `Err`/panic stubs are emitted — builders are infallible):
//!
//! - **Explicit placeholders** kept only for discriminant stability:
//!   `Deprecated95`, `Deprecated96`, `Deprecated102`, `Deprecated103`,
//!   `Deprecated111`.
//! - **Deprecated handlers** that return `DoubleZeroError::Deprecated`:
//!   `ActivateDevice`, `RejectDevice`, `SuspendDevice`, `ResumeDevice`,
//!   `CloseAccountDevice`; the corresponding link lifecycle variants
//!   (`ActivateLink`, `RejectLink`, `SuspendLink`, `ResumeLink`,
//!   `CloseAccountLink`); the corresponding user lifecycle variants
//!   (`ActivateUser`, `RejectUser`, `SuspendUser`, `ResumeUser`,
//!   `CloseAccountUser`, `BanUser`); the multicast lifecycle variants
//!   (`ActivateMulticastGroup`, `RejectMulticastGroup`,
//!   `DeactivateMulticastGroup`); the device-allowlist / user-allowlist toggles
//!   (`AddDeviceAllowlist`, `RemoveDeviceAllowlist`, `AddUserAllowlist`,
//!   `RemoveUserAllowlist`); and the deprecated `*DeviceInterface` variants
//!   (`ActivateDeviceInterface`, `RemoveDeviceInterface`,
//!   `UnlinkDeviceInterface`, `RejectDeviceInterface`).
//!
//! Later rollout PRs (RFC-26 R1–R9) add the remaining domains; R0 ships the
//! scaffold plus four exemplar builders that establish the pattern.

mod common;

pub mod contributor;
pub mod device;
pub mod exchange;
pub mod link;
pub mod location;
pub mod multicastgroup;
pub mod user;

pub use common::{compute_budget_prelude, MAX_COMPUTE_UNIT_LIMIT, MAX_HEAP_FRAME_BYTES};
