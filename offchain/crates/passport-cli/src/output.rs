//! Shared output-format helpers for the read verbs (`fetch`, `find-validator`).
//!
//! The `--json` / `--json-compact` flags are additive: when neither is set the
//! verbs reproduce the exact pre-RFC-20 human-readable output. The output format
//! is the single source of truth carried on [`CliContext::output_format`]; these
//! helpers keep the JSON-emission logic in one place.

use std::io::Write;

use doublezero_cli_core::OutputFormat;
use serde::Serialize;

use crate::error::Result;

/// True when the resolved format requests JSON output.
pub fn is_json(format: OutputFormat) -> bool {
    matches!(format, OutputFormat::Json | OutputFormat::JsonCompact)
}

/// Serialize `value` as JSON (compact or pretty per `format`), terminated by a
/// newline.
pub fn emit_json<W: Write, T: Serialize>(
    out: &mut W,
    value: &T,
    format: OutputFormat,
) -> Result<()> {
    let rendered = if matches!(format, OutputFormat::JsonCompact) {
        serde_json::to_string(value)?
    } else {
        serde_json::to_string_pretty(value)?
    };
    writeln!(out, "{rendered}")?;
    Ok(())
}
