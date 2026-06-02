//! Top-level passport subcommand enum, mounted by the binary.

use std::io::Write;

use clap::Subcommand;
use doublezero_cli_core::CliContext;

use crate::{error::Result, fetch, find_validator, prepare_access, request_access};

/// The passport module's verbs. Variant names and their argument surfaces match
/// the pre-RFC-20 `doublezero-solana passport` commands one-for-one, so the
/// user-facing CLI is unchanged.
#[derive(Debug, Subcommand)]
pub enum Command {
    /// Fetch and display the current program configuration and access request (if any)
    Fetch(fetch::FetchArgs),
    /// Find and display the Current Identity
    FindValidator(find_validator::FindValidatorArgs),
    /// Validate arguments and generate the required transaction signature command
    PrepareValidatorAccess(prepare_access::PrepareValidatorAccessArgs),
    /// Request access as a Solana Validator
    RequestValidatorAccess(request_access::RequestValidatorAccessArgs),
}

impl Command {
    /// Dispatch to the selected verb. All output is written to `out`; all
    /// configuration is read from `ctx`.
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        match self {
            Command::Fetch(args) => args.execute(ctx, out).await,
            Command::FindValidator(args) => args.execute(ctx, out).await,
            Command::PrepareValidatorAccess(args) => args.execute(ctx, out).await,
            Command::RequestValidatorAccess(args) => args.execute(ctx, out).await,
        }
    }
}
