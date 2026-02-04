use crate::command::util;
use clap::Args;
use doublezero_cli::{
    checkversion::{get_version_status, VersionStatus},
    doublezerocommand::CliCommand,
};
use doublezero_sdk::ProgramVersion;
use serde::{Deserialize, Serialize};

use crate::{
    requirements::check_doublezero,
    routes::retrieve_routes,
    servicecontroller::{RouteRecord, ServiceControllerImpl},
};

#[derive(Args, Debug)]
pub struct RoutesCliCommand {
    /// Output as json
    #[arg(long, default_value = "false")]
    json: bool,
}

/// JSON response wrapper that includes version status information
#[derive(Debug, Serialize, Deserialize)]
struct RoutesJsonResponse {
    version: VersionStatus,
    routes: Vec<RouteRecord>,
}

impl RoutesCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);
        check_doublezero(&controller, client, None).await?;

        // Get version status for JSON output
        let version_status = get_version_status(client, ProgramVersion::current());

        let routes = retrieve_routes(&controller, None).await?;

        if self.json {
            // For JSON output, include version status in the response
            let json_response = RoutesJsonResponse {
                version: version_status,
                routes,
            };
            let output = serde_json::to_string_pretty(&json_response)?;
            println!("{output}");
        } else {
            // For table output, print version warning to stderr if needed
            if let Some(msg) = version_status.message() {
                eprintln!("{msg}");
            }
            util::show_output(routes, false)?;
        }

        Ok(())
    }
}
