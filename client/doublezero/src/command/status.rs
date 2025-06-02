use crate::requirements::check_doublezero;
use crate::servicecontroller::{ServiceController, ServiceControllerImpl};
use clap::Args;
use doublezero_cli::{
    doublezerocommand::CliCommand,
    helpers::{init_command, print_error},
};
use tabled::{settings::Style, Table};

#[derive(Args, Debug)]
pub struct StatusCliCommand {}

impl StatusCliCommand {
    pub async fn execute(self, _client: &dyn CliCommand) -> eyre::Result<()> {
        let spinner = init_command();
        let controller = ServiceControllerImpl::new(None);

        // Check requirements
        check_doublezero(Some(&spinner))?;

        match controller.status().await {
            Err(e) => {
                spinner.finish_and_clear();
                print_error(e)
            }
            Ok(status_responses) => {
                if !status_responses.is_empty() {
                    let table = Table::new(status_responses)
                        .with(Style::psql().remove_horizontals())
                        .to_string();
                    spinner.finish_and_clear();
                    println!("{}", table);
                }
            }
        }

        Ok(())
    }
}
