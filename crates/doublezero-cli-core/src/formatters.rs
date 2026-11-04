//! Shared display formatters used by CLI verbs to render table output.
//!
//! Per RFC-20 (§Output conventions): "Modules SHOULD use shared display
//! helpers from the CLI core crate for common types (pubkey, bandwidth,
//! latency, IPv4)." This module is the home for those helpers. It also
//! hosts the cross-verb rendering helpers used by every list/get verb
//! (`render_collection`, `render_record`) and the write-verb tail helpers
//! (`print_signature`, `print_signature_and_then`), and the address-label
//! helpers (`format_labeled_address`, `label_addresses`, `labeled_table`).

use std::{
    collections::HashMap,
    fmt::{self, Display},
    io::Write,
};

use serde::Serialize;
use solana_sdk::signature::Signature;
use tabled::{
    settings::{object::Rows, Format, Style},
    Table, Tabled,
};

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

/// Format an address for display, naming it by its label when it has one.
///
/// Matches the Solana CLI's `format_labeled_address`: a labeled address reads
/// `label (ABCD..WXYZ)`, with the label cut to 31 characters and the address
/// shortened to its first and last four characters. An unlabeled address is
/// returned unchanged.
pub fn format_labeled_address(pubkey: &str, address_labels: &HashMap<String, String>) -> String {
    match address_labels.get(pubkey) {
        Some(label) => {
            let tail = pubkey.char_indices().rev().nth(3).map_or(0, |(i, _)| i);
            format!("{label:.31} ({pubkey:.4}..{})", &pubkey[tail..])
        }
        None => pubkey.to_string(),
    }
}

/// Replace every labeled address in `text` with [`format_labeled_address`].
///
/// An address is a whole run of ASCII alphanumeric characters (base58 uses
/// no others), so a cell holding one pubkey, a comma-separated list of them,
/// or a pubkey inside other text (`solana_validator: <pubkey>`) is labeled,
/// while a label key never matches part of a longer token.
pub fn label_addresses(text: &str, address_labels: &HashMap<String, String>) -> String {
    if address_labels.is_empty() {
        return text.to_string();
    }
    let mut out = String::with_capacity(text.len());
    let mut rest = text;
    while let Some(start) = rest.find(|c: char| c.is_ascii_alphanumeric()) {
        out.push_str(&rest[..start]);
        let token = &rest[start..];
        let end = token
            .find(|c: char| !c.is_ascii_alphanumeric())
            .unwrap_or(token.len());
        out.push_str(&format_labeled_address(&token[..end], address_labels));
        rest = &token[end..];
    }
    out.push_str(rest);
    out
}

/// Build a table from `rows` with every labeled address in its body cells
/// shown by [`label_addresses`]; the header row is left alone. The labels are
/// applied before the table is styled or rendered, so column widths fit them.
pub fn labeled_table<I, T>(rows: I, address_labels: &HashMap<String, String>) -> Table
where
    I: IntoIterator<Item = T>,
    T: Tabled,
{
    let mut table = Table::new(rows);
    if !address_labels.is_empty() {
        table.modify(
            Rows::new(1..),
            Format::content(|s| label_addresses(s, address_labels)),
        );
    }
    table
}

/// Render a collection of records using the resolved [`OutputFormat`].
///
/// Used by every list verb. The shape is the pre-refactor block:
/// pretty JSON for `--json`, single-line JSON for `--json-compact`, and a
/// `psql`-styled table without horizontal separators otherwise. Table output
/// shows labeled addresses by their label; JSON keeps the raw pubkeys.
pub fn render_collection<T, W>(
    out: &mut W,
    rows: Vec<T>,
    format: OutputFormat,
    address_labels: &HashMap<String, String>,
) -> eyre::Result<()>
where
    T: Tabled + Serialize,
    W: Write,
{
    let rendered = match format {
        OutputFormat::Json => serde_json::to_string_pretty(&rows)?,
        OutputFormat::JsonCompact => serde_json::to_string(&rows)?,
        OutputFormat::Table => labeled_table(rows, address_labels)
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
/// Table output shows labeled addresses by their label; JSON keeps the raw
/// pubkeys.
pub fn render_record<T, W>(
    out: &mut W,
    record: &T,
    format: OutputFormat,
    address_labels: &HashMap<String, String>,
) -> eyre::Result<()>
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
                let value = label_addresses(value, address_labels);
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
        render_collection(&mut out, rows, OutputFormat::Table, &HashMap::new()).unwrap();
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
        render_collection(&mut out, rows, OutputFormat::Json, &HashMap::new()).unwrap();
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
        render_collection(&mut out, rows, OutputFormat::JsonCompact, &HashMap::new()).unwrap();
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
        render_record(&mut out, &row, OutputFormat::Table, &HashMap::new()).unwrap();
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
        render_record(&mut out, &row, OutputFormat::Json, &HashMap::new()).unwrap();
        let s = String::from_utf8(out).unwrap();
        let parsed: serde_json::Value = serde_json::from_str(s.trim()).unwrap();
        assert_eq!(parsed["code"], "abc");
        assert_eq!(parsed["value"], 42);
    }

    const LABELED: &str = "DZfLKFDgLShjY34WqXdVVzHUvVtrYXb7UtdrALnGa8jw";

    fn labels(label: &str) -> HashMap<String, String> {
        HashMap::from([(LABELED.to_string(), label.to_string())])
    }

    #[test]
    fn format_labeled_address_shows_label_and_short_key() {
        assert_eq!(
            format_labeled_address(LABELED, &labels("Jared")),
            "Jared (DZfL..a8jw)"
        );
    }

    #[test]
    fn format_labeled_address_leaves_unlabeled_key() {
        let other = "11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo";
        assert_eq!(format_labeled_address(other, &labels("Jared")), other);
        assert_eq!(format_labeled_address(LABELED, &HashMap::new()), LABELED);
    }

    #[test]
    fn format_labeled_address_truncates_label_to_31_chars() {
        let long = "a-label-that-is-much-longer-than-thirty-one-characters";
        assert_eq!(
            format_labeled_address(LABELED, &labels(long)),
            "a-label-that-is-much-longer-tha (DZfL..a8jw)"
        );
    }

    #[test]
    fn format_labeled_address_handles_short_keys() {
        let labels = HashMap::from([("ab".to_string(), "short".to_string())]);
        assert_eq!(format_labeled_address("ab", &labels), "short (ab..ab)");
    }

    #[test]
    fn label_addresses_replaces_whole_tokens_only() {
        let labels = labels("Jared");
        assert_eq!(
            label_addresses(&format!("solana_validator: {LABELED}"), &labels),
            "solana_validator: Jared (DZfL..a8jw)"
        );
        assert_eq!(
            label_addresses(&format!("{LABELED}, {LABELED}"), &labels),
            "Jared (DZfL..a8jw), Jared (DZfL..a8jw)"
        );
        let longer = format!("{LABELED}x");
        assert_eq!(label_addresses(&longer, &labels), longer);
        assert_eq!(label_addresses("", &labels), "");
    }

    #[derive(Tabled, Serialize)]
    struct OwnerRow {
        code: String,
        owner: String,
    }

    #[test]
    fn render_collection_labels_table_but_not_json() {
        let rows = || {
            vec![OwnerRow {
                code: "a".to_string(),
                owner: LABELED.to_string(),
            }]
        };
        let mut out = Vec::new();
        render_collection(&mut out, rows(), OutputFormat::Table, &labels("Jared")).unwrap();
        let s = String::from_utf8(out).unwrap();
        assert_eq!(
            s,
            " code | owner              \n a    | Jared (DZfL..a8jw) \n"
        );

        let mut out = Vec::new();
        render_collection(
            &mut out,
            rows(),
            OutputFormat::JsonCompact,
            &labels("Jared"),
        )
        .unwrap();
        let s = String::from_utf8(out).unwrap();
        assert_eq!(s, format!("[{{\"code\":\"a\",\"owner\":\"{LABELED}\"}}]\n"));
    }

    #[test]
    fn render_record_labels_table_values() {
        let row = OwnerRow {
            code: "a".to_string(),
            owner: LABELED.to_string(),
        };
        let mut out = Vec::new();
        render_record(&mut out, &row, OutputFormat::Table, &labels("Jared")).unwrap();
        let s = String::from_utf8(out).unwrap();
        assert_eq!(s, " code  | a\n owner | Jared (DZfL..a8jw)\n");
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
