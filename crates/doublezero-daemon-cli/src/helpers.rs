//! Shared output helpers for daemon-control verbs.

use std::io::Write;

use tabled::{settings::Style, Table, Tabled};

/// Render a list of records as either pretty-printed JSON or a psql-style table.
pub fn show_output<T, W: Write>(data: Vec<T>, is_output_json: bool, out: &mut W) -> eyre::Result<()>
where
    T: serde::Serialize + Tabled,
{
    let output = if is_output_json {
        serde_json::to_string_pretty(&data)?
    } else {
        Table::new(data)
            .with(Style::psql().remove_horizontals())
            .to_string()
    };
    writeln!(out, "{output}")?;
    Ok(())
}
