use std::{fs, path::Path};

use anyhow::Result;
use doublezero_contributor_rewards::{
    cli::snapshot::{CompleteSnapshot, SnapshotMetadata},
    ingestor::{
        epoch::LeaderSchedule,
        types::{FetchData, apply_json_compat_migrations},
    },
};
use serde_json::Value;

// validate() is what both producers run before they save, so these pin the leader
// schedule checks it has to enforce.
fn load_snapshot(leader_schedule: Option<LeaderSchedule>) -> Result<CompleteSnapshot> {
    let mut data: Value = serde_json::from_str(&fs::read_to_string(Path::new(
        "tests/testnet_snapshot.json",
    ))?)?;
    apply_json_compat_migrations(&mut data);
    let fetch_data: FetchData = serde_json::from_value(data)?;

    let metadata = SnapshotMetadata {
        created_at: "2026-01-01T00:00:00Z".to_string(),
        network: "Testnet".to_string(),
        exchanges_count: fetch_data.dz_serviceability.exchanges.len(),
        locations_count: fetch_data.dz_serviceability.locations.len(),
        devices_count: fetch_data.dz_serviceability.devices.len(),
        internet_samples_count: fetch_data.dz_internet.internet_latency_samples.len(),
        device_samples_count: fetch_data.dz_telemetry.device_latency_samples.len(),
    };

    Ok(CompleteSnapshot {
        dz_epoch: 89,
        solana_epoch: leader_schedule
            .as_ref()
            .map(|schedule| schedule.solana_epoch),
        fetch_data,
        leader_schedule,
        metadata,
    })
}

fn load_leader_schedule() -> Result<LeaderSchedule> {
    let data: Value = serde_json::from_str(&fs::read_to_string(Path::new(
        "tests/leader-schedule-epoch-89.json",
    ))?)?;
    Ok(serde_json::from_value(data)?)
}

#[test]
fn test_validate_accepts_a_complete_snapshot() -> Result<()> {
    let snapshot = load_snapshot(Some(load_leader_schedule()?))?;

    snapshot.validate()?;

    Ok(())
}

#[test]
fn test_validate_rejects_a_missing_leader_schedule() -> Result<()> {
    let error = load_snapshot(None)?.validate().unwrap_err().to_string();

    assert!(error.contains("Missing leader schedule"), "{error}");

    Ok(())
}

#[test]
fn test_validate_rejects_an_empty_leader_schedule() -> Result<()> {
    let mut schedule = load_leader_schedule()?;
    schedule.schedule_map.clear();

    let error = load_snapshot(Some(schedule))?
        .validate()
        .unwrap_err()
        .to_string();

    assert!(error.contains("Leader schedule is empty"), "{error}");

    Ok(())
}
