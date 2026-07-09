use std::net::Ipv4Addr;

use crate::doublezerocommand::CliCommand;
use doublezero_sdk::{
    commands::{
        accesspass::get::GetAccessPassCommand,
        allowlist::foundation::list::ListFoundationAllowlistCommand,
    },
    get_doublezero_pubkey,
};
use indicatif::ProgressBar;

pub const CHECK_ID_JSON: u8 = 1;
pub const CHECK_BALANCE: u8 = 2;
pub const CHECK_FOUNDATION_ALLOWLIST: u8 = 4;

pub fn check_requirements(
    client: &dyn CliCommand,
    spinner: Option<&ProgressBar>,
    checks: u8,
) -> eyre::Result<()> {
    // Check that have your id.json (skip if alternative keypair source is available)
    if (checks & CHECK_ID_JSON != 0) && !client.has_keypair_source() {
        check_id(spinner)?;
    }

    // Check that have some balance
    if checks & CHECK_BALANCE != 0 {
        check_balance(client, spinner)?;
    }

    if checks & CHECK_FOUNDATION_ALLOWLIST != 0 {
        check_allowlist(client, spinner, checks)?;
    }

    Ok(())
}

pub fn check_id(spinner: Option<&ProgressBar>) -> eyre::Result<()> {
    match get_doublezero_pubkey() {
        Ok(_) => Ok(()),
        Err(_) => {
            let error_msg =
                "DoubleZero keypair not found at default location (~/.config/doublezero/id.json)";
            if let Some(spinner) = spinner {
                spinner.println(format!("    Error: {error_msg}"));
            } else {
                tracing::error!("{error_msg}");
            }

            Err(eyre::eyre!(
                "Provide keypair via:\n  \
                 - doublezero --keypair /path/to/key.json\n  \
                 - cat key.json | doublezero ...\n  \
                 - export DOUBLEZERO_KEYPAIR=/path/to/key.json\n  \
                 - doublezero keygen"
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
                    tracing::error!("Insufficient balance");
                }
                eyre::bail!(
                    "This DoubleZero account has no available credits. Please recharge your account. [{}].",
                    client.get_payer().to_string()
                );
            }

            Ok(())
        }
        Err(e) => Err(eyre::eyre!("Unable to get balance: {:?}", e)),
    }
}

/// Mirrors `check_accesspass` in `doublezero-daemon-cli`
/// (`crates/doublezero-daemon-cli/src/connect.rs`) — keep the two in sync if
/// AccessPass validity semantics change.
pub fn check_accesspass(
    client: &dyn CliCommand,
    client_ip: Ipv4Addr,
    enforce_epoch: bool,
) -> eyre::Result<bool> {
    // A missing AccessPass returns Ok(false) (not an error) so the caller can
    // render its own diagnostic (e.g. the client IP and UserPayer) before
    // bailing, rather than surfacing a generic "not found" message.
    let Some((_, accesspass)) = client.get_accesspass(GetAccessPassCommand {
        client_ip,
        user_payer: client.get_payer(),
    })?
    else {
        return Ok(false);
    };

    if !enforce_epoch {
        return Ok(true);
    }
    let epoch = client.get_epoch()?;
    Ok(accesspass.last_access_epoch >= epoch)
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
    } else {
        false
    };

    if !is_in_allowlist {
        if let Some(spinner) = spinner {
            spinner.println("You are not authorized to connect");
        } else {
            tracing::error!("You are not authorized to connect");
        }
        eyre::bail!("Please contact the DoubleZero Foundation to allow you to connect.");
    }

    Ok(())
}
