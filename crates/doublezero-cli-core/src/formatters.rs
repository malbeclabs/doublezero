//! Shared display formatters used by CLI verbs to render table output.
//!
//! Per RFC-20 (§Output conventions): "Modules SHOULD use shared display
//! helpers from the CLI core crate for common types (pubkey, bandwidth,
//! latency, IPv4)." This module is the home for those helpers. It also
//! hosts the cross-verb rendering helpers used by every list/get verb
//! (`render_collection`, `render_record`) and the write-verb tail helpers
//! (`print_signature`, `print_signature_and_then`).

use std::{
    fmt::{self, Display},
    io::Write,
};

use serde::Serialize;
use solana_sdk::signature::Signature;
use tabled::{settings::Style, Table, Tabled};

use crate::context::OutputFormat;

pub struct DisplayVec<'a, T: Display>(pub &'a Vec<T>);

impl<'a, T: Display> Display for DisplayVec<'a, T> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let mut iter = self.0.iter();
        if let Some(first) = iter.next() {
            write!(f, "{first}")?;
            for item in iter {
                write!(f, ",{item}")?;
            }
        }
        Ok(())
    }
}

impl<'a, T: Display> From<&'a Vec<T>> for DisplayVec<'a, T> {
    fn from(vec: &'a Vec<T>) -> Self {
        DisplayVec(vec)
    }
}

pub fn stringify_vec<T: Display>(v: &Vec<T>) -> String {
    format!("{}", DisplayVec(v))
}

/// Render a collection of records using the resolved [`OutputFormat`].
///
/// Used by every list verb. The shape is the pre-refactor block:
/// pretty JSON for `--json`, single-line JSON for `--json-compact`, and a
/// `psql`-styled table without horizontal separators otherwise.
pub fn render_collection<T, W>(out: &mut W, rows: Vec<T>, format: OutputFormat) -> eyre::Result<()>
where
    T: Tabled + Serialize,
    W: Write,
{
    let rendered = match format {
        OutputFormat::Json => serde_json::to_string_pretty(&rows)?,
        OutputFormat::JsonCompact => serde_json::to_string(&rows)?,
        OutputFormat::Table => Table::new(rows)
            .with(Style::psql().remove_horizontals())
            .to_string(),
    };
    writeln!(out, "{rendered}")?;
    Ok(())
}

/// Render a single record using the resolved [`OutputFormat`].
///
/// Used by every get verb. Table output emits `header | value` rows aligned
/// to the longest header, matching the pre-refactor `location/get` layout.
pub fn render_record<T, W>(out: &mut W, record: &T, format: OutputFormat) -> eyre::Result<()>
where
    T: Tabled + Serialize,
    W: Write,
{
    match format {
        OutputFormat::Json => {
            let json = serde_json::to_string_pretty(record)?;
            writeln!(out, "{json}")?;
        }
        OutputFormat::JsonCompact => {
            let json = serde_json::to_string(record)?;
            writeln!(out, "{json}")?;
        }
        OutputFormat::Table => {
            let headers = T::headers();
            let fields = record.fields();
            let max_len = headers.iter().map(|h| h.len()).max().unwrap_or(0);
            for (header, value) in headers.iter().zip(fields.iter()) {
                writeln!(out, " {header:<max_len$} | {value}")?;
            }
        }
    }
    Ok(())
}

/// Standard write-verb tail: print the on-chain transaction signature.
///
/// Output shape matches the pre-refactor `writeln!(out, "Signature: {sig}")`
/// so existing tests keep passing byte-for-byte.
pub fn print_signature<W: Write>(out: &mut W, signature: &Signature) -> eyre::Result<()> {
    writeln!(out, "Signature: {signature}")?;
    Ok(())
}

/// Variant of [`print_signature`] for verbs that follow the signature line
/// with additional output (typically `--wait` polling). The supplied closure
/// receives the same writer.
pub fn print_signature_and_then<W, F>(
    out: &mut W,
    signature: &Signature,
    after: F,
) -> eyre::Result<()>
where
    W: Write,
    F: FnOnce(&mut W) -> eyre::Result<()>,
{
    writeln!(out, "Signature: {signature}")?;
    after(out)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde::Serialize;
    use tabled::Tabled;

    #[test]
    fn display_vec_joins_with_comma() {
        let v = vec![1, 2, 3];
        assert_eq!(stringify_vec(&v), "1,2,3");
    }

    #[test]
    fn display_vec_handles_empty() {
        let v: Vec<i32> = vec![];
        assert_eq!(stringify_vec(&v), "");
    }

    #[test]
    fn display_vec_handles_singleton() {
        let v = vec!["only"];
        assert_eq!(stringify_vec(&v), "only");
    }

    #[derive(Tabled, Serialize)]
    struct Row {
        code: String,
        value: u32,
    }

    #[test]
    fn render_collection_table_matches_legacy_layout() {
        let rows = vec![Row {
            code: "a".to_string(),
            value: 1,
        }];
        let mut out = Vec::new();
        render_collection(&mut out, rows, OutputFormat::Table).unwrap();
        let s = String::from_utf8(out).unwrap();
        assert_eq!(s, " code | value \n a    | 1     \n");
    }

    #[test]
    fn render_collection_json_pretty() {
        let rows = vec![Row {
            code: "a".to_string(),
            value: 1,
        }];
        let mut out = Vec::new();
        render_collection(&mut out, rows, OutputFormat::Json).unwrap();
        let s = String::from_utf8(out).unwrap();
        let parsed: serde_json::Value = serde_json::from_str(s.trim()).unwrap();
        assert_eq!(parsed[0]["code"], "a");
        assert_eq!(parsed[0]["value"], 1);
        assert!(s.contains('\n'), "pretty JSON should be multi-line");
    }

    #[test]
    fn render_collection_json_compact_single_line() {
        let rows = vec![Row {
            code: "a".to_string(),
            value: 1,
        }];
        let mut out = Vec::new();
        render_collection(&mut out, rows, OutputFormat::JsonCompact).unwrap();
        let s = String::from_utf8(out).unwrap();
        assert_eq!(s, "[{\"code\":\"a\",\"value\":1}]\n");
    }

    #[test]
    fn render_record_table_pads_to_longest_header() {
        let row = Row {
            code: "abc".to_string(),
            value: 42,
        };
        let mut out = Vec::new();
        render_record(&mut out, &row, OutputFormat::Table).unwrap();
        let s = String::from_utf8(out).unwrap();
        assert_eq!(s, " code  | abc\n value | 42\n");
    }

    #[test]
    fn render_record_json_emits_object() {
        let row = Row {
            code: "abc".to_string(),
            value: 42,
        };
        let mut out = Vec::new();
        render_record(&mut out, &row, OutputFormat::Json).unwrap();
        let s = String::from_utf8(out).unwrap();
        let parsed: serde_json::Value = serde_json::from_str(s.trim()).unwrap();
        assert_eq!(parsed["code"], "abc");
        assert_eq!(parsed["value"], 42);
    }

    #[test]
    fn print_signature_matches_legacy_format() {
        let sig = Signature::default();
        let mut out = Vec::new();
        print_signature(&mut out, &sig).unwrap();
        let s = String::from_utf8(out).unwrap();
        assert_eq!(s, format!("Signature: {sig}\n"));
    }

    #[test]
    fn print_signature_and_then_runs_closure() {
        let sig = Signature::default();
        let mut out = Vec::new();
        print_signature_and_then(&mut out, &sig, |out| {
            writeln!(out, "Status: ready")?;
            Ok(())
        })
        .unwrap();
        let s = String::from_utf8(out).unwrap();
        assert_eq!(s, format!("Signature: {sig}\nStatus: ready\n"));
    }
}
