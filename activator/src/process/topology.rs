use doublezero_sdk::{
    commands::{
        device::list::ListDeviceCommand,
        topology::{backfill::BackfillTopologyCommand, list::ListTopologyCommand},
    },
    DeviceStatus, DoubleZeroClient, InterfaceStatus, InterfaceType, LoopbackType,
};
use log::{error, info};
use solana_sdk::pubkey::Pubkey;

/// Handle a topology account event by backfilling all devices that have activated
/// Vpnv4 loopbacks but do not yet have a FlexAlgoNodeSegment for this topology.
///
/// Called when a new topology is created or updated. The BackfillTopology instruction
/// is idempotent: it skips devices that already have a segment for this topology.
pub fn process_topology_event(client: &dyn DoubleZeroClient, topology_name: &str) {
    info!("Event:Topology name={topology_name} — backfilling devices");

    let devices = match ListDeviceCommand.execute(client) {
        Ok(d) => d,
        Err(e) => {
            error!("Failed to list devices for topology backfill: {e}");
            return;
        }
    };

    let device_pubkeys: Vec<Pubkey> = devices
        .iter()
        .filter(|(_, d)| {
            matches!(
                d.status,
                DeviceStatus::Activated
                    | DeviceStatus::DeviceProvisioning
                    | DeviceStatus::LinkProvisioning
                    | DeviceStatus::Drained
            )
        })
        .filter(|(_, d)| {
            d.interfaces.iter().any(|iface| {
                let iface = iface.into_current_version();
                iface.interface_type == InterfaceType::Loopback
                    && iface.loopback_type == LoopbackType::Vpnv4
                    && iface.status == InterfaceStatus::Activated
            })
        })
        .map(|(pk, _)| *pk)
        .collect();

    if device_pubkeys.is_empty() {
        info!("No eligible devices found for topology backfill (topology={topology_name})");
        return;
    }

    info!(
        "Backfilling {} device(s) for topology={topology_name}",
        device_pubkeys.len()
    );

    let cmd = BackfillTopologyCommand {
        name: topology_name.to_string(),
        device_pubkeys,
    };

    match cmd.execute(client) {
        Ok(sig) => info!("BackfillTopology({topology_name}) succeeded: {sig}"),
        Err(e) => error!("BackfillTopology({topology_name}) failed: {e}"),
    }
}

/// Backfill all known topologies for a single device, called after a Vpnv4 loopback
/// is activated on that device. The BackfillTopology instruction is idempotent.
pub fn backfill_all_topologies_for_device(client: &dyn DoubleZeroClient, device_pubkey: &Pubkey) {
    let topologies = match ListTopologyCommand.execute(client) {
        Ok(t) => t,
        Err(e) => {
            error!("Failed to list topologies for device backfill: {e}");
            return;
        }
    };

    if topologies.is_empty() {
        return;
    }

    for topology in topologies.values() {
        info!(
            "Backfilling topology={} for device={}",
            topology.name, device_pubkey
        );

        let cmd = BackfillTopologyCommand {
            name: topology.name.clone(),
            device_pubkeys: vec![*device_pubkey],
        };

        match cmd.execute(client) {
            Ok(sig) => info!(
                "BackfillTopology({}) for device {} succeeded: {sig}",
                topology.name, device_pubkey
            ),
            Err(e) => error!(
                "BackfillTopology({}) for device {} failed: {e}",
                topology.name, device_pubkey
            ),
        }
    }
}
