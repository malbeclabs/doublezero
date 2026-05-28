//! Preflight requirement bitflags.
//!
//! Per RFC-20 (§Error handling and requirements): "Preflight checks compose
//! from a shared bitflag type defined in the CLI core crate (keypair
//! available, payer has balance, payer on allowlist, ...)."
//!
//! Bit values are kept aligned with the legacy `u8` constants in
//! `smartcontract/cli/src/requirements.rs` so the two representations are
//! interchangeable during the opportunistic migration described by RFC-20:
//!
//! | Flag                  | Bit value | Legacy constant            |
//! | --------------------- | --------- | -------------------------- |
//! | `KEYPAIR`             | `0b0001`  | `CHECK_ID_JSON`            |
//! | `BALANCE`             | `0b0010`  | `CHECK_BALANCE`            |
//! | `FOUNDATION_ALLOWLIST`| `0b0100`  | `CHECK_FOUNDATION_ALLOWLIST` |
//!
//! This crate intentionally does **not** ship a `check_requirements`
//! implementation: the balance and allowlist checks require a typed
//! per-module backend client, which lives in the module crate. Modules
//! consume `RequirementCheck` as the canonical bitflag set and provide
//! their own dispatch.

use bitflags::bitflags;

bitflags! {
    /// Preflight checks a verb requests at the top of `execute`.
    #[derive(Debug, Clone, Copy, PartialEq, Eq)]
    pub struct RequirementCheck: u8 {
        /// Verify that a keypair source is available (CLI flag, env var, or
        /// piped stdin).
        const KEYPAIR = 0b0000_0001;
        /// Verify that the payer account has a non-zero balance.
        const BALANCE = 0b0000_0010;
        /// Verify that the payer is on the foundation allowlist.
        const FOUNDATION_ALLOWLIST = 0b0000_0100;
    }
}

impl From<u8> for RequirementCheck {
    fn from(bits: u8) -> Self {
        Self::from_bits_truncate(bits)
    }
}

impl From<RequirementCheck> for u8 {
    fn from(flags: RequirementCheck) -> Self {
        flags.bits()
    }
}

/// Run preflight checks at the top of a verb's `execute` body.
///
/// The macro expands to a single call to `$client.check_requirements(bits)?`
/// where `bits` is the `u8` projection of `$flags`. This keeps verb bodies to
/// one line and lets the existing module trait method (which today takes a
/// `u8`) stay unchanged through the migration described by RFC-20: when the
/// trait method is later flipped to accept `RequirementCheck` directly, only
/// this macro changes.
///
/// `$flags` MUST evaluate to a [`RequirementCheck`]. A type annotation in the
/// expansion enforces this at the call site instead of silently coercing
/// integer literals.
///
/// ```ignore
/// use doublezero_cli_core::{require, RequirementCheck};
/// # struct Client;
/// # impl Client { fn check_requirements(&self, _: u8) -> eyre::Result<()> { Ok(()) } }
/// # fn body(client: &Client) -> eyre::Result<()> {
/// require!(client, RequirementCheck::KEYPAIR | RequirementCheck::BALANCE);
/// # Ok(()) }
/// ```
#[macro_export]
macro_rules! require {
    ($client:expr, $flags:expr) => {{
        let __dz_flags: $crate::RequirementCheck = $flags;
        $client.check_requirements(__dz_flags.bits())?
    }};
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn bit_values_match_legacy_u8_constants() {
        assert_eq!(RequirementCheck::KEYPAIR.bits(), 1);
        assert_eq!(RequirementCheck::BALANCE.bits(), 2);
        assert_eq!(RequirementCheck::FOUNDATION_ALLOWLIST.bits(), 4);
    }

    #[test]
    fn round_trips_via_u8() {
        let checks = RequirementCheck::KEYPAIR | RequirementCheck::BALANCE;
        let bits: u8 = checks.into();
        let back: RequirementCheck = bits.into();
        assert_eq!(back, checks);
    }
}
