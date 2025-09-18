use crate::{
    command::util,
    requirements::check_doublezero,
    servicecontroller::{
        DoubleZeroStatus, ServiceController, ServiceControllerImpl, ServiceStatus,
    },
};
use clap::Args;
use doublezero_cli::{doublezerocommand::CliCommand, helpers::print_error};
use doublezero_config::Environment;

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
        check_doublezero(&controller, None)?;

        match controller.status().await {
            Err(e) => print_error(e),
            Ok(status_response) => {
                let env = client.get_environment();
                let daemon_env = Environment::from_program_id(&status_response.program_id)?;

                if env != daemon_env {
                    eyre::bail!(
                        "Environment mismatch: CLI is set to {env}, but daemon is set to {daemon_env}. Please reconfigure with: doublezero config set --env [mainnet-beta|testnet]",
                    );
                }

                let results: Vec<ServiceStatus> = if status_response.results.is_empty() {
                    vec![ServiceStatus {
                        doublezero_status: DoubleZeroStatus {
                            session_status: "disconnected".to_string(),
                            ..Default::default()
                        },
                        ..Default::default()
                    }]
                } else {
                    status_response.results
                };

                util::show_output(results, self.json)?
            }
        }

        Ok(())
    }
}
