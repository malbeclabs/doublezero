use crate::servicecontroller::ServiceController;
use doublezero_cli::doublezerocommand::CliCommand;
use indicatif::ProgressBar;

pub async fn check_doublezero<T: ServiceController>(
    controller: &T,
    client: &dyn CliCommand,
    spinner: Option<&ProgressBar>,
) -> eyre::Result<()> {
    if !controller.service_controller_check() {
        if let Some(spinner) = spinner {
            spinner.println("doublezero service is not accessible.");
        } else {
            eprintln!("doublezero service is not accessible.");
        }

        eyre::bail!("Please start the doublezerod service.");
    }

    // Check that the doublezerod is accessible
    if !controller.service_controller_can_open() {
        if let Some(spinner) = spinner {
            spinner.println("doublezero service is not accessible.");
        } else {
            eprintln!("doublezero service is not accessible.");
        }
        eyre::bail!("Please check the permissions of the doublezerod service.");
    }

    let deamon_env = controller.get_env().await?;
    if deamon_env != client.get_environment() {
        return Err(eyre::eyre!(
            "The client and the daemon are using different environments.\n\
Client: {}\n\
Daemon: {}\n\
Please update the daemon configuration so both use the same environment.",
            client.get_environment(),
            deamon_env
        ));
    }

    Ok(())
}
