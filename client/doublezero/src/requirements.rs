use crate::servicecontroller::ServiceController;
use indicatif::ProgressBar;

pub fn check_doublezero<T: ServiceController>(
    controller: &T,
    spinner: Option<&ProgressBar>,
) -> eyre::Result<()> {
    if !controller.service_controller_check() {
        if let Some(spinner) = spinner {
            spinner.println("doublezero service is not accessible.");
        } else {
            eprintln!("doublezero service is not accessible.");
        }

        return Err(eyre::eyre!("Please start the doublezerod service."));
    }

    // Check that the doublezerod is accessible
    if !controller.service_controller_can_open() {
        if let Some(spinner) = spinner {
            spinner.println("doublezero service is not accessible.");
        } else {
            eprintln!("doublezero service is not accessible.");
        }
        return Err(eyre::eyre!(
            "Please check the permissions of the doublezerod service."
        ));
    }

    Ok(())
}
