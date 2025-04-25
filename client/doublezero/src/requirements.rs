use indicatif::ProgressBar;
use std::path::Path;
use std::fs::File;

pub fn check_doublezero(spinner: Option<&ProgressBar>) -> eyre::Result<()> {
    if !service_controller_check() {
        if let Some(spinner) = spinner {
            spinner.println("doublezero service is not accessible.");
        } else {
            eprintln!("doublezero service is not accessible.");
        }

        return Err(eyre::eyre!("Please start the doublezerod service."));
    }

    // Check that the doublezerod is accessible
    if !service_controller_can_open() {
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

pub fn service_controller_check() -> bool {
    Path::new("/var/run/doublezerod/doublezerod.sock").exists()
}

pub fn service_controller_can_open() -> bool {
    let file = File::options()
        .read(true)
        .write(true)
        .open("/var/run/doublezerod/doublezerod.sock");
    match file {
        Ok(_) => true,
        Err(e) => !matches!(e.kind(), std::io::ErrorKind::PermissionDenied),
    }
}
