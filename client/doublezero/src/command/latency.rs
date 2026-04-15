use crate::command::util;
use clap::Args;
use doublezero_cli::doublezerocommand::CliCommand;
use doublezero_sdk::commands::device::list::ListDeviceCommand;
use indicatif::{ProgressBar, ProgressStyle};
use std::time::Duration;

use crate::{
    dzd_latency::retrieve_latencies, requirements::check_doublezero,
    servicecontroller::ServiceControllerImpl,
};

#[derive(Args, Debug)]
pub struct LatencyCliCommand {
    /// Output as json
    #[arg(long, default_value = "false")]
    json: bool,
}

impl LatencyCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);

        let spinner = ProgressBar::new_spinner();
        spinner.set_style(
            ProgressStyle::default_spinner()
                .template("{spinner:.green} [{elapsed_precise}] {msg}")
                .expect("Failed to set template")
                .tick_strings(&["-", "\\", "|", "/"]),
        );
        spinner.enable_steady_tick(Duration::from_millis(100));
        spinner.set_message("Checking daemon...");

        check_doublezero(&controller, client, Some(&spinner)).await?;

        spinner.set_message("Fetching devices...");
        let devices = client.list_device(ListDeviceCommand)?;

        let latencies = retrieve_latencies(&controller, &devices, false, Some(&spinner)).await?;

        spinner.finish_and_clear();
        util::show_output(latencies, self.json)?;

        Ok(())
    }
}
