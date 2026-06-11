use clap::Args;
use serde::Serialize;
use tabled::{
    settings::{object::Columns, Alignment, Style},
    Table, Tabled,
};

#[derive(Debug, Args)]
pub struct OutputOptions {
    /// Output as JSON instead of a table.
    #[arg(long)]
    pub json: bool,
}

/// Print a collection of rows as a markdown table, or as JSON if `json` is set.
/// `right_aligned` specifies column indices that should be right-aligned.
pub fn print_table(
    rows: Vec<impl Tabled + Serialize>,
    options: &OutputOptions,
    right_aligned: &[usize],
) {
    if options.json {
        println!(
            "{}",
            serde_json::to_string_pretty(&rows).expect("JSON serialization")
        );
        return;
    }

    if rows.is_empty() {
        return;
    }

    let mut table = Table::new(rows);
    table.with(Style::markdown());
    for &col in right_aligned {
        table.modify(Columns::new(col..=col), Alignment::right());
    }
    println!("{table}");
}
