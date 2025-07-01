use crate::doublezerocommand::CliCommand;
use doublezero_sdk::{
    commands::allowlist::{
        device::list::ListDeviceAllowlistCommand, foundation::list::ListFoundationAllowlistCommand,
        user::list::ListUserAllowlistCommand,
    },
    get_doublezero_pubkey,
};
use indicatif::ProgressBar;

pub const CHECK_ID_JSON: u8 = 1;
pub const CHECK_BALANCE: u8 = 2;
pub const CHECK_FOUNDATION_ALLOWLIST: u8 = 4;
pub const CHECK_DEVICE_ALLOWLIST: u8 = 8;
pub const CHECK_USER_ALLOWLIST: u8 = 16;

pub fn check_requirements(
    client: &dyn CliCommand,
    spinner: Option<&ProgressBar>,
    checks: u8,
) -> eyre::Result<()> {
    // Check that have your id.json
    if checks & CHECK_ID_JSON != 0 {
        check_id(spinner)?;
    }

    // Check that have some balance
    if checks & CHECK_BALANCE != 0 {
        check_balance(client, spinner)?;
    }

    if checks & CHECK_FOUNDATION_ALLOWLIST != 0
        || checks & CHECK_DEVICE_ALLOWLIST != 0
        || checks & CHECK_USER_ALLOWLIST != 0
    {
        check_allowlist(client, spinner, checks)?;
    }

    Ok(())
}

pub fn check_id(spinner: Option<&ProgressBar>) -> eyre::Result<()> {
    match get_doublezero_pubkey() {
        Ok(_) => Ok(()),
        Err(_) => {
            if let Some(spinner) = spinner {
                spinner.println(format!(
                    "    {}: DoubleZero id.json not found (~/.config/doublezero/id.json)",
                    "Error"
                ));
            } else {
                eprintln!("DoubleZero id.json not found (~/.config/doublezero/id.json)",);
            }

            Err(eyre::eyre!(
                "Please create a new id.json (doublezero keygen) and transfer balance."
            ))
        }
    }
}

pub fn check_balance(client: &dyn CliCommand, spinner: Option<&ProgressBar>) -> eyre::Result<()> {
    match client.get_balance() {
        Ok(balance) => {
            // Check that have some balance
            if balance == 0 {
                if let Some(spinner) = spinner {
                    spinner.println("Insufficient balance");
                } else {
                    eprintln!("Insufficient balance");
                }
                eyre::bail!(
                    "Please transfer some balance to your DoubleZero account [{}].",
                    client.get_payer().to_string()
                );
            }

            Ok(())
        }
        Err(e) => Err(eyre::eyre!("Unable to get balance: {:?}", e)),
    }
}

pub fn check_allowlist(
    client: &dyn CliCommand,
    spinner: Option<&ProgressBar>,
    checks: u8,
) -> eyre::Result<()> {
    // Check that the client is in the allowlist
    let is_in_allowlist = if checks & CHECK_FOUNDATION_ALLOWLIST != 0 {
        let allowlist = client.list_foundation_allowlist(ListFoundationAllowlistCommand)?;
        allowlist.contains(&client.get_payer())
    } else if checks & CHECK_DEVICE_ALLOWLIST != 0 {
        let allowlist = client.list_device_allowlist(ListDeviceAllowlistCommand)?;
        allowlist.contains(&client.get_payer())
    } else if checks & CHECK_USER_ALLOWLIST != 0 {
        let allowlist = client.list_user_allowlist(ListUserAllowlistCommand)?;
        allowlist.contains(&client.get_payer())
    } else {
        false
    };

    if !is_in_allowlist {
        if let Some(spinner) = spinner {
            spinner.println("You are not in the allowlist");
        } else {
            eprintln!("Error: You are not in the allowlist");
        }
        eyre::bail!("Please contact the DoubleZero Foundation to add you to the allowlist.");
    }

    Ok(())
}
