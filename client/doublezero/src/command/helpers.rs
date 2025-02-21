use std::time::Duration;

use indicatif::{ProgressBar, ProgressStyle};

pub fn init_command() -> ProgressBar {
    let spinner = ProgressBar::new_spinner();

    spinner.set_style(
        ProgressStyle::default_spinner()
            .template("{spinner:.green}  {msg}")
            .expect("Failed to set template")
            .tick_strings(&["-", "\\", "|", "/"]),
    );
    spinner.enable_steady_tick(Duration::from_millis(100));

    spinner.println("DoubleZero Service Provisioning");

    spinner
}