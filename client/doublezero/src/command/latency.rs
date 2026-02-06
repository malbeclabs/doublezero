use crate::command::util;
use clap::Args;
use doublezero_cli::doublezerocommand::CliCommand;
use doublezero_sdk::commands::device::list::ListDeviceCommand;

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
        check_doublezero(&controller, client, None).await?;

        let devices = client.list_device(ListDeviceCommand)?;
        let latencies = retrieve_latencies(&controller, &devices, false, None).await?;

        util::show_output(latencies, self.json)?;

        Ok(())
    }
}
