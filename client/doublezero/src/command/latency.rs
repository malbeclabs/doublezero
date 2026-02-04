use crate::command::util;
use clap::Args;
use doublezero_cli::{
    checkversion::{get_version_status, VersionStatus},
    doublezerocommand::CliCommand,
};
use doublezero_sdk::{commands::device::list::ListDeviceCommand, ProgramVersion};
use serde::{Deserialize, Serialize};

use crate::{
    dzd_latency::retrieve_latencies,
    requirements::check_doublezero,
    servicecontroller::{LatencyRecord, ServiceControllerImpl},
};

#[derive(Args, Debug)]
pub struct LatencyCliCommand {
    /// Output as json
    #[arg(long, default_value = "false")]
    json: bool,
}

/// JSON response wrapper that includes version status information
#[derive(Debug, Serialize, Deserialize)]
struct LatencyJsonResponse {
    version: VersionStatus,
    latencies: Vec<LatencyRecord>,
}

impl LatencyCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);
        check_doublezero(&controller, client, None).await?;

        // Get version status for JSON output
        let version_status = get_version_status(client, ProgramVersion::current());

        let devices = client.list_device(ListDeviceCommand)?;
        let latencies = retrieve_latencies(&controller, &devices, false, None).await?;

        if self.json {
            // For JSON output, include version status in the response
            let json_response = LatencyJsonResponse {
                version: version_status,
                latencies,
            };
            let output = serde_json::to_string_pretty(&json_response)?;
            println!("{output}");
        } else {
            // For table output, print version warning to stderr if needed
            if let Some(msg) = version_status.message() {
                eprintln!("{msg}");
            }
            util::show_output(latencies, false)?;
        }

        Ok(())
    }
}
