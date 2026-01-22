use doublezero_sdk::{
    commands::{
        device::get::GetDeviceCommand, link::get::GetLinkCommand,
        multicastgroup::get::GetMulticastGroupCommand, user::get::GetUserCommand,
    },
    CurrentInterfaceVersion, Device, DeviceStatus, InterfaceStatus, Link, LinkStatus,
    MulticastGroup, MulticastGroupStatus, User, UserStatus,
};
use solana_sdk::pubkey::Pubkey;

use crate::doublezerocommand::CliCommand;

pub fn poll_for_device_activated(
    client: &dyn CliCommand,
    device_pubkey: &Pubkey,
) -> eyre::Result<Device> {
    let start_time = std::time::Instant::now();
    let timeout = std::time::Duration::from_secs(60);
    let poll_interval = std::time::Duration::from_secs(1);
    let mut last_error: Option<eyre::Error> = None;

    loop {
        if start_time.elapsed() >= timeout {
            return Err(match last_error {
                Some(e) => eyre::eyre!(
                    "Timeout waiting for device activation after {} seconds. Last error: {}",
                    timeout.as_secs(),
                    e
                ),
                None => eyre::eyre!(
                    "Timeout waiting for device activation after {} seconds",
                    timeout.as_secs()
                ),
            });
        }

        match client.get_device(GetDeviceCommand {
            pubkey_or_code: device_pubkey.to_string(),
        }) {
            Ok((_, device)) => {
                if device.status == DeviceStatus::DeviceProvisioning
                    || device.status == DeviceStatus::Activated
                    || device.status == DeviceStatus::Rejected
                {
                    return Ok(device);
                }
            }
            Err(e) => {
                // Device not found or some other error, continue polling
                // It may take some time for the device to be visible onchain after the creation
                // transaction is confirmed, so we need to poll here until is is.
                last_error = Some(e);
            }
        }

        std::thread::sleep(poll_interval);
    }
}

pub fn poll_for_device_interface_activated(
    client: &dyn CliCommand,
    device_pubkey: &Pubkey,
    interface_name: &str,
) -> eyre::Result<CurrentInterfaceVersion> {
    let start_time = std::time::Instant::now();
    let timeout = std::time::Duration::from_secs(20);
    let poll_interval = std::time::Duration::from_secs(1);
    let mut last_error: Option<eyre::Error> = None;

    loop {
        if start_time.elapsed() >= timeout {
            return Err(match last_error {
                Some(e) => eyre::eyre!(
                    "Timeout waiting for device activation after 20 seconds. Last error: {}",
                    e
                ),
                None => eyre::eyre!("Timeout waiting for device activation after 20 seconds"),
            });
        }

        match client.get_device(GetDeviceCommand {
            pubkey_or_code: device_pubkey.to_string(),
        }) {
            Ok((_, device)) => {
                if let Some(iface) = device.interfaces.iter().find(|iface| {
                    iface.into_current_version().name.to_lowercase()
                        == interface_name.to_lowercase()
                }) {
                    let current = iface.into_current_version();
                    if current.status != InterfaceStatus::Pending {
                        return Ok(current);
                    } else {
                        last_error = Some(eyre::eyre!(
                            "Interface '{}' found but not activated (status: {:?})",
                            interface_name,
                            current.status
                        ));
                    }
                } else {
                    last_error = Some(eyre::eyre!(
                        "Interface '{}' not found on device '{}'",
                        interface_name,
                        device_pubkey
                    ));
                }
            }
            Err(e) => {
                last_error = Some(e);
            }
        }

        std::thread::sleep(poll_interval);
    }
}

pub fn poll_for_link_activated(
    client: &dyn CliCommand,
    link_pubkey: &Pubkey,
) -> eyre::Result<Link> {
    let start_time = std::time::Instant::now();
    let timeout = std::time::Duration::from_secs(60);
    let poll_interval = std::time::Duration::from_secs(1);
    let mut last_error: Option<eyre::Error> = None;

    loop {
        if start_time.elapsed() >= timeout {
            return Err(match last_error {
                Some(e) => eyre::eyre!(
                    "Timeout waiting for link activation after {} seconds. Last error: {}",
                    timeout.as_secs(),
                    e
                ),
                None => eyre::eyre!(
                    "Timeout waiting for link activation after {} seconds",
                    timeout.as_secs()
                ),
            });
        }

        match client.get_link(GetLinkCommand {
            pubkey_or_code: link_pubkey.to_string(),
        }) {
            Ok((_, link)) => {
                if link.status == LinkStatus::Provisioning
                    || link.status == LinkStatus::Activated
                    || link.status == LinkStatus::Rejected
                {
                    return Ok(link);
                }
            }
            Err(e) => {
                // Link not found or some other error, continue polling
                // It may take some time for the link to be visible onchain after the creation
                // transaction is confirmed, so we need to poll here until is is.
                last_error = Some(e);
            }
        }

        std::thread::sleep(poll_interval);
    }
}

pub fn poll_for_user_activated(
    client: &dyn CliCommand,
    user_pubkey: &Pubkey,
) -> eyre::Result<User> {
    let start_time = std::time::Instant::now();
    let timeout = std::time::Duration::from_secs(60);
    let poll_interval = std::time::Duration::from_secs(1);
    let mut last_error: Option<eyre::Error> = None;

    loop {
        if start_time.elapsed() >= timeout {
            return Err(match last_error {
                Some(e) => eyre::eyre!(
                    "Timeout waiting for user activation after {} seconds. Last error: {}",
                    timeout.as_secs(),
                    e
                ),
                None => eyre::eyre!(
                    "Timeout waiting for user activation after {} seconds",
                    timeout.as_secs()
                ),
            });
        }

        match client.get_user(GetUserCommand {
            pubkey: *user_pubkey,
        }) {
            Ok((_, user)) => {
                if user.status == UserStatus::Activated || user.status == UserStatus::Rejected {
                    return Ok(user);
                }
            }
            Err(e) => {
                // User not found or some other error, continue polling
                // It may take some time for the user to be visible onchain after the creation
                // transaction is confirmed, so we need to poll here until is is.
                last_error = Some(e);
            }
        }

        std::thread::sleep(poll_interval);
    }
}

pub fn poll_for_multicastgroup_activated(
    client: &dyn CliCommand,
    mgroup_pubkey: &Pubkey,
) -> eyre::Result<MulticastGroup> {
    let start_time = std::time::Instant::now();
    let timeout = std::time::Duration::from_secs(60);
    let poll_interval = std::time::Duration::from_secs(1);
    let mut last_error: Option<eyre::Error> = None;

    loop {
        if start_time.elapsed() >= timeout {
            return Err(match last_error {
                Some(e) => eyre::eyre!(
                    "Timeout waiting for multicast group activation after {} seconds. Last error: {}",
                    timeout.as_secs(),
                    e
                ),
                None => eyre::eyre!("Timeout waiting for multicast group activation after {} seconds", 
                    timeout.as_secs()
                ),
            });
        }

        match client.get_multicastgroup(GetMulticastGroupCommand {
            pubkey_or_code: mgroup_pubkey.to_string(),
        }) {
            Ok((_, mgroup)) => {
                if mgroup.status == MulticastGroupStatus::Activated
                    || mgroup.status == MulticastGroupStatus::Rejected
                {
                    return Ok(mgroup);
                }
            }
            Err(e) => {
                // Multicast group not found or some other error, continue polling
                // It may take some time for the multicast group to be visible onchain after the creation
                // transaction is confirmed, so we need to poll here until is is.
                last_error = Some(e);
            }
        }

        std::thread::sleep(poll_interval);
    }
}
