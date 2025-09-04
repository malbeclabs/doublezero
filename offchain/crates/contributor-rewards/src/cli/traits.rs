use crate::cli::common::OutputFormat;
use anyhow::Result;
use serde::Serialize;

/// Trait for types that can be exported to various formats
pub trait Exportable {
    fn export(&self, format: OutputFormat) -> Result<String>;

    /// Default implementation for CSV export
    fn to_csv(&self) -> Result<String>
    where
        Self: Serialize,
    {
        let mut wtr = csv::Writer::from_writer(vec![]);
        wtr.serialize(self)?;
        let data = wtr.into_inner()?;
        Ok(String::from_utf8(data)?)
    }

    /// Default implementation for JSON export
    fn to_json(&self, pretty: bool) -> Result<String>
    where
        Self: Serialize,
    {
        if pretty {
            Ok(serde_json::to_string_pretty(self)?)
        } else {
            Ok(serde_json::to_string(self)?)
        }
    }
}
