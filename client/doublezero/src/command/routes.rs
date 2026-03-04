use crate::command::util;
use clap::Args;

use doublezero_cli::doublezerocommand::CliCommand;

use crate::{
    requirements::check_doublezero, routes::retrieve_routes,
    servicecontroller::ServiceControllerImpl,
};

#[derive(Args, Debug)]
pub struct RoutesCliCommand {
    /// Output as json
    #[arg(long, default_value = "false")]
    json: bool,
}

impl RoutesCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);
        check_doublezero(&controller, client, None).await?;

        let routes = retrieve_routes(&controller, None).await?;
        util::show_output(routes, self.json)?;

        Ok(())
    }
}
