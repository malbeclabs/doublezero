use crate::{
    command::util,
    requirements::check_doublezero,
    servicecontroller::{ServiceController, ServiceControllerImpl},
};
use clap::Args;
use doublezero_cli::{doublezerocommand::CliCommand, helpers::print_error};

#[derive(Args, Debug)]
pub struct StatusCliCommand {
    /// Output as json
    #[arg(long, default_value = "false")]
    json: bool,
}

impl StatusCliCommand {
    pub async fn execute(self, client: &dyn CliCommand) -> eyre::Result<()> {
        let controller = ServiceControllerImpl::new(None);

        // Check requirements
        check_doublezero(&controller, client, None).await?;

        match controller.status().await {
            Err(e) => print_error(e),
            Ok(status_responses) => {
                if !status_responses.is_empty() {
                    util::show_output(status_responses, self.json)?
                }
            }
        }

        Ok(())
    }
}
