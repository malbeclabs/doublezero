use crate::{
    command::util,
    requirements::check_doublezero,
    servicecontroller::{ServiceController, ServiceControllerImpl},
};
use clap::Args;
use doublezero_cli::{
    doublezerocommand::CliCommand,
    helpers::{init_command, print_error},
};

#[derive(Args, Debug)]
pub struct StatusCliCommand {
    /// Output as json
    #[arg(long, default_value = "false")]
    json: bool,
}

impl StatusCliCommand {
    pub async fn execute(self, _client: &dyn CliCommand) -> eyre::Result<()> {
        let spinner = init_command();
        let controller = ServiceControllerImpl::new(None);

        // Check requirements
        check_doublezero(&controller, Some(&spinner))?;

        match controller.status().await {
            Err(e) => {
                spinner.finish_and_clear();
                print_error(e)
            }
            Ok(status_responses) => {
                if !status_responses.is_empty() {
                    spinner.finish_and_clear();
                    util::show_output(status_responses, self.json)?
                }
            }
        }

        Ok(())
    }
}
