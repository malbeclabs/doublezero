use crate::cli::{
    common::{OutputFormat, collection_to_csv, to_json_string},
    traits::Exportable,
};
use anyhow::Result;
use contributor_rewards::processor::{
    internet::InternetTelemetryStats, telemetry::DZDTelemetryStats,
};

// Implement Exportable for processor types

impl Exportable for InternetTelemetryStats {
    fn export(&self, format: OutputFormat) -> Result<String> {
        match format {
            OutputFormat::Csv => self.to_csv(),
            OutputFormat::Json => self.to_json(false),
            OutputFormat::JsonPretty => self.to_json(true),
        }
    }
}

impl Exportable for Vec<InternetTelemetryStats> {
    fn export(&self, format: OutputFormat) -> Result<String> {
        match format {
            OutputFormat::Csv => collection_to_csv(self),
            OutputFormat::Json => to_json_string(self, false),
            OutputFormat::JsonPretty => to_json_string(self, true),
        }
    }
}

impl Exportable for DZDTelemetryStats {
    fn export(&self, format: OutputFormat) -> Result<String> {
        match format {
            OutputFormat::Csv => self.to_csv(),
            OutputFormat::Json => self.to_json(false),
            OutputFormat::JsonPretty => self.to_json(true),
        }
    }
}

impl Exportable for Vec<DZDTelemetryStats> {
    fn export(&self, format: OutputFormat) -> Result<String> {
        match format {
            OutputFormat::Csv => collection_to_csv(self),
            OutputFormat::Json => to_json_string(self, false),
            OutputFormat::JsonPretty => to_json_string(self, true),
        }
    }
}
