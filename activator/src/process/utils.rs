use doublezero_program_common::types::NetworkV4;
use doublezero_sdk::{
    commands::device::interface::{
        activate::ActivateDeviceInterfaceCommand, reject::RejectDeviceInterfaceCommand,
        remove::RemoveDeviceInterfaceCommand, unlink::UnlinkDeviceInterfaceCommand,
    },
    DoubleZeroClient,
};
use log::error;
use solana_sdk::pubkey::Pubkey;

pub fn activate_interface(
    client: &dyn DoubleZeroClient,
    device_pubkey: &Pubkey,
    iface_dev_or_link: &str,
    iface_name: &str,
    ip_net: &NetworkV4,
    node_segment_idx: u16,
) {
    let res = ActivateDeviceInterfaceCommand {
        pubkey: *device_pubkey,
        name: iface_name.to_string(),
        ip_net: *ip_net,
        node_segment_idx,
    }
    .execute(client);

    if let Err(e) = res {
        error!(
            "Failed to update interface status to activated for {} on {}: {}",
            iface_name, iface_dev_or_link, e
        );
    }
}

pub fn unlink_interface(
    client: &dyn DoubleZeroClient,
    device_pubkey: &Pubkey,
    iface_dev_or_link: &str,
    iface_name: &str,
) {
    let res = UnlinkDeviceInterfaceCommand {
        pubkey: *device_pubkey,
        name: iface_name.to_string(),
    }
    .execute(client);

    if let Err(e) = res {
        error!(
            "Failed to update interface status to unlinked for {} on {}: {}",
            iface_name, iface_dev_or_link, e
        );
    }
}

pub fn reject_interface(
    client: &dyn DoubleZeroClient,
    device_pubkey: &Pubkey,
    iface_dev_or_link: &str,
    iface_name: &str,
) {
    let res = RejectDeviceInterfaceCommand {
        pubkey: *device_pubkey,
        name: iface_name.to_string(),
    }
    .execute(client);

    if let Err(e) = res {
        error!(
            "Failed to update interface status to rejected for {} on {}: {}",
            iface_name, iface_dev_or_link, e
        );
    }
}

pub fn remove_interface(
    client: &dyn DoubleZeroClient,
    device_pubkey: &Pubkey,
    iface_dev_or_link: &str,
    iface_name: &str,
) {
    let res = RemoveDeviceInterfaceCommand {
        pubkey: *device_pubkey,
        name: iface_name.to_string(),
    }
    .execute(client);

    if let Err(e) = res {
        error!(
            "Failed to remove interface {} on {}: {}",
            iface_name, iface_dev_or_link, e
        );
    }
}
