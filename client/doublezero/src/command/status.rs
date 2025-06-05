use crate::{requirements::check_doublezero, servicecontroller::ServiceController};
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
        let controller = ServiceController::new(None);

        // Check requirements
        check_doublezero(Some(&spinner))?;

        match controller.status().await {
            Err(e) => print_error(e),
            Ok(status_responses) => {
                if !status_responses.is_empty() {
                    let table = Table::new(status_responses)
                        .with(Style::psql().remove_horizontals())
                        .to_string();
                    println!("{}", table);
                }
            }
        }

        Ok(())
    }
}
