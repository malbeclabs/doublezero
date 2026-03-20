use clap::{Args, Subcommand};
use doublezero_cli::{
    device::{
        create::CreateDeviceCliCommand,
        delete::DeleteDeviceCliCommand,
        get::GetDeviceCliCommand,
        interface::{
            create::CreateDeviceInterfaceCliCommand, delete::DeleteDeviceInterfaceCliCommand,
            get::GetDeviceInterfaceCliCommand, list::ListDeviceInterfaceCliCommand,
            update::UpdateDeviceInterfaceCliCommand,
        },
        list::ListDeviceCliCommand,
        update::UpdateDeviceCliCommand,
    },
    doublezerocommand::CliCommand,
};
use doublezero_sdk::{
    commands::{
        device::{list::ListDeviceCommand, update::UpdateDeviceCommand},
        user::list::ListUserCommand,
    },
    UserStatus, UserType,
};
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, io::Write};

#[derive(Debug, Subcommand)]
pub enum InterfaceCommands {
    /// Create a new device interface
    #[clap()]
    Create(CreateDeviceInterfaceCliCommand),
    /// Update an existing device interface
    #[clap()]
    Update(UpdateDeviceInterfaceCliCommand),
    /// List all device interfaces for a given device
    #[clap()]
    List(ListDeviceInterfaceCliCommand),
    /// Get details for a specific device interface
    #[clap()]
    Get(GetDeviceInterfaceCliCommand),
    /// Delete a device interface
    #[clap()]
    Delete(DeleteDeviceInterfaceCliCommand),
}

#[derive(Args, Debug)]
pub struct InterfaceCliCommand {
    #[command(subcommand)]
    pub command: InterfaceCommands,
}

#[derive(Args, Debug)]
pub struct DeviceCliCommand {
    #[command(subcommand)]
    pub command: DeviceCommands,
}

#[derive(Debug, Subcommand)]
pub enum DeviceCommands {
    /// Create a new device
    #[clap()]
    Create(CreateDeviceCliCommand),
    /// Update an existing device
    #[clap()]
    Update(UpdateDeviceCliCommand),
    /// List all devices
    #[clap()]
    List(ListDeviceCliCommand),
    /// Get details for a specific device
    #[clap()]
    Get(GetDeviceCliCommand),
    /// Delete a device
    #[clap()]
    Delete(DeleteDeviceCliCommand),
    /// Correct stale multicast subscriber/publisher counts on all devices (one-time migration)
    #[clap()]
    MigrateMulticastCounts(MigrateMulticastCountsCommand),
    /// Correct stale unicast user counts on all devices
    #[clap()]
    MigrateUnicastCounts(MigrateUnicastCountsCommand),
    /// Interface commands
    #[clap()]
    Interface(InterfaceCliCommand),
}

#[derive(Args, Debug)]
pub struct MigrateMulticastCountsCommand {
    /// Print what would be corrected without submitting transactions
    #[arg(long, default_value_t = false)]
    pub dry_run: bool,
}

impl MigrateMulticastCountsCommand {
    pub fn execute<C: CliCommand, W: Write>(&self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Tally live multicast publishers and subscribers per device.
        let users = client.list_user(ListUserCommand)?;
        let mut per_device: HashMap<Pubkey, (u16, u16)> = HashMap::new(); // (publishers, subscribers)
        for user in users.values() {
            let is_live = !matches!(
                user.status,
                UserStatus::Rejected | UserStatus::Banned | UserStatus::PendingBan
            );
            if user.user_type == UserType::Multicast && is_live {
                let (pub_count, sub_count) = per_device.entry(user.device_pk).or_default();
                if user.is_publisher() {
                    *pub_count = pub_count.saturating_add(1);
                } else {
                    *sub_count = sub_count.saturating_add(1);
                }
            }
        }

        // Find devices with stale counts.
        let devices = client.list_device(ListDeviceCommand)?;
        let mut corrections_needed: Vec<(Pubkey, String, u16, u16, u16, u16)> = vec![];
        for (device_pubkey, device) in &devices {
            let (actual_pub, actual_sub) = per_device.get(device_pubkey).copied().unwrap_or((0, 0));
            if device.multicast_publishers_count != actual_pub
                || device.multicast_subscribers_count != actual_sub
            {
                corrections_needed.push((
                    *device_pubkey,
                    device.code.clone(),
                    device.multicast_subscribers_count,
                    actual_sub,
                    device.multicast_publishers_count,
                    actual_pub,
                ));
            }
        }

        if corrections_needed.is_empty() {
            writeln!(out, "0 device(s) require correction")?;
            return Ok(());
        }

        // Print what needs correcting (always, even in dry-run).
        for (pubkey, code, old_sub, new_sub, old_pub, new_pub) in &corrections_needed {
            writeln!(
                out,
                "device {code} ({pubkey}): subscribers {old_sub} -> {new_sub}, publishers {old_pub} -> {new_pub}"
            )?;
        }

        if self.dry_run {
            writeln!(out, "[dry-run] no transactions sent.")?;
            return Ok(());
        }

        // Submit corrections.
        let mut corrected = 0u32;
        for (pubkey, code, _, actual_sub, _, actual_pub) in &corrections_needed {
            let result = client.update_device(UpdateDeviceCommand {
                pubkey: *pubkey,
                code: None,
                device_type: None,
                public_ip: None,
                dz_prefixes: None,
                metrics_publisher: None,
                contributor_pk: None,
                location_pk: None,
                mgmt_vrf: None,
                interfaces: None,
                max_users: None,
                users_count: None,
                status: None,
                desired_status: None,
                reference_count: None,
                max_unicast_users: None,
                unicast_users_count: None,
                max_multicast_subscribers: None,
                max_multicast_publishers: None,
                multicast_subscribers_count: Some(*actual_sub),
                multicast_publishers_count: Some(*actual_pub),
            });
            match result {
                Ok(sig) => {
                    corrected += 1;
                    writeln!(out, "corrected {code} ({pubkey}): {sig}")?;
                }
                Err(e) => {
                    writeln!(out, "WARNING: failed to correct {code} ({pubkey}): {e}")?;
                }
            }
        }
        writeln!(out, "{corrected} device(s) corrected")?;
        Ok(())
    }
}

#[derive(Args, Debug)]
pub struct MigrateUnicastCountsCommand {
    /// Print what would be corrected without submitting transactions
    #[arg(long, default_value_t = false)]
    pub dry_run: bool,
}

impl MigrateUnicastCountsCommand {
    pub fn execute<C: CliCommand, W: Write>(&self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Tally live unicast users per device.
        let users = client.list_user(ListUserCommand)?;
        let mut per_device: HashMap<Pubkey, u16> = HashMap::new();
        for user in users.values() {
            let is_live = !matches!(
                user.status,
                UserStatus::Rejected | UserStatus::Banned | UserStatus::PendingBan
            );
            if user.user_type != UserType::Multicast && is_live {
                let count = per_device.entry(user.device_pk).or_default();
                *count = count.saturating_add(1);
            }
        }

        // Find devices with stale counts.
        let devices = client.list_device(ListDeviceCommand)?;
        let mut corrections_needed: Vec<(Pubkey, String, u16, u16)> = vec![];
        for (device_pubkey, device) in &devices {
            let actual = per_device.get(device_pubkey).copied().unwrap_or(0);
            if device.unicast_users_count != actual {
                corrections_needed.push((
                    *device_pubkey,
                    device.code.clone(),
                    device.unicast_users_count,
                    actual,
                ));
            }
        }

        if corrections_needed.is_empty() {
            writeln!(out, "0 device(s) require correction")?;
            return Ok(());
        }

        // Print what needs correcting (always, even in dry-run).
        for (pubkey, code, old, new) in &corrections_needed {
            writeln!(
                out,
                "device {code} ({pubkey}): unicast_users_count {old} -> {new}"
            )?;
        }

        if self.dry_run {
            writeln!(out, "[dry-run] no transactions sent.")?;
            return Ok(());
        }

        // Submit corrections.
        let mut corrected = 0u32;
        for (pubkey, code, _, actual) in &corrections_needed {
            let result = client.update_device(UpdateDeviceCommand {
                pubkey: *pubkey,
                code: None,
                device_type: None,
                public_ip: None,
                dz_prefixes: None,
                metrics_publisher: None,
                contributor_pk: None,
                location_pk: None,
                mgmt_vrf: None,
                interfaces: None,
                max_users: None,
                users_count: None,
                status: None,
                desired_status: None,
                reference_count: None,
                max_unicast_users: None,
                unicast_users_count: Some(*actual),
                max_multicast_subscribers: None,
                max_multicast_publishers: None,
                multicast_subscribers_count: None,
                multicast_publishers_count: None,
            });
            match result {
                Ok(sig) => {
                    corrected += 1;
                    writeln!(out, "corrected {code} ({pubkey}): {sig}")?;
                }
                Err(e) => {
                    writeln!(out, "WARNING: failed to correct {code} ({pubkey}): {e}")?;
                }
            }
        }
        writeln!(out, "{corrected} device(s) corrected")?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_cli::tests::utils::create_test_client;
    use doublezero_sdk::{Device, DeviceStatus, DeviceType, User, UserCYOA, UserStatus, UserType};
    use doublezero_serviceability::state::device::{DeviceDesiredStatus, DeviceHealth};
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::collections::HashMap;

    fn make_device(sub_count: u16, pub_count: u16) -> Device {
        Device {
            account_type: doublezero_sdk::AccountType::Device,
            owner: Pubkey::new_unique(),
            index: 0,
            reference_count: 0,
            bump_seed: 0,
            contributor_pk: Pubkey::new_unique(),
            location_pk: Pubkey::new_unique(),
            exchange_pk: Pubkey::new_unique(),
            device_type: DeviceType::Hybrid,
            public_ip: [192, 168, 1, 1].into(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            code: "testdevice".to_string(),
            dz_prefixes: "10.0.0.1/32".parse().unwrap(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: DeviceHealth::ReadyForUsers,
            desired_status: DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_subscribers_count: sub_count,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: pub_count,
            max_multicast_publishers: 0,
        }
    }

    fn make_device_unicast(unicast_count: u16) -> Device {
        Device {
            unicast_users_count: unicast_count,
            multicast_subscribers_count: 0,
            multicast_publishers_count: 0,
            ..make_device(0, 0)
        }
    }

    fn make_unicast_user(device_pk: Pubkey) -> User {
        User {
            user_type: UserType::IBRL,
            publishers: vec![],
            subscribers: vec![],
            ..make_multicast_user(device_pk, false)
        }
    }

    fn make_multicast_user(device_pk: Pubkey, is_publisher: bool) -> User {
        User {
            account_type: doublezero_sdk::AccountType::User,
            owner: Pubkey::new_unique(),
            index: 0,
            bump_seed: 0,
            user_type: UserType::Multicast,
            tenant_pk: Pubkey::new_unique(),
            device_pk,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: [192, 168, 1, 1].into(),
            dz_ip: std::net::Ipv4Addr::UNSPECIFIED,
            tunnel_id: 0,
            tunnel_net: Default::default(),
            status: UserStatus::Activated,
            publishers: if is_publisher {
                vec![Pubkey::new_unique()]
            } else {
                vec![]
            },
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            multicast_publisher: false,
        }
    }

    #[test]
    fn test_migrate_no_op_when_counts_correct() {
        let mut client = create_test_client();
        let device_pubkey = Pubkey::new_unique();
        let device = make_device(3, 2);

        let mut users: HashMap<Pubkey, User> = HashMap::new();
        for _ in 0..3 {
            users.insert(
                Pubkey::new_unique(),
                make_multicast_user(device_pubkey, false),
            );
        }
        for _ in 0..2 {
            users.insert(
                Pubkey::new_unique(),
                make_multicast_user(device_pubkey, true),
            );
        }
        let devices: HashMap<Pubkey, Device> = HashMap::from([(device_pubkey, device)]);

        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));
        client
            .expect_list_device()
            .returning(move |_| Ok(devices.clone()));
        client.expect_update_device().times(0);

        let mut out = Vec::new();
        let res = MigrateMulticastCountsCommand { dry_run: false }.execute(&client, &mut out);
        assert!(res.is_ok());
        let output = String::from_utf8(out).unwrap();
        assert!(output.contains("0 device(s)"));
    }

    #[test]
    fn test_migrate_corrects_stale_counts() {
        let mut client = create_test_client();
        let device_pubkey = Pubkey::new_unique();
        // Stale: sub=5, pub=0. Actual: sub=3, pub=2.
        let device = make_device(5, 0);

        let mut users: HashMap<Pubkey, User> = HashMap::new();
        for _ in 0..3 {
            users.insert(
                Pubkey::new_unique(),
                make_multicast_user(device_pubkey, false),
            );
        }
        for _ in 0..2 {
            users.insert(
                Pubkey::new_unique(),
                make_multicast_user(device_pubkey, true),
            );
        }
        let devices: HashMap<Pubkey, Device> = HashMap::from([(device_pubkey, device)]);

        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));
        client
            .expect_list_device()
            .returning(move |_| Ok(devices.clone()));
        client
            .expect_update_device()
            .times(1)
            .returning(|_| Ok(Signature::new_unique()));

        let mut out = Vec::new();
        let res = MigrateMulticastCountsCommand { dry_run: false }.execute(&client, &mut out);
        assert!(res.is_ok());
        let output = String::from_utf8(out).unwrap();
        assert!(output.contains("1 device(s) corrected"));
    }

    #[test]
    fn test_migrate_failure_on_one_device_does_not_prevent_others() {
        let mut client = create_test_client();
        let device_pk_a = Pubkey::new_unique();
        let device_pk_b = Pubkey::new_unique();

        let mut users: HashMap<Pubkey, User> = HashMap::new();
        users.insert(
            Pubkey::new_unique(),
            make_multicast_user(device_pk_a, false),
        );
        users.insert(Pubkey::new_unique(), make_multicast_user(device_pk_a, true));
        users.insert(
            Pubkey::new_unique(),
            make_multicast_user(device_pk_b, false),
        );
        users.insert(Pubkey::new_unique(), make_multicast_user(device_pk_b, true));

        let devices: HashMap<Pubkey, Device> = HashMap::from([
            (device_pk_a, make_device(5, 0)),
            (device_pk_b, make_device(5, 0)),
        ]);

        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));
        client
            .expect_list_device()
            .returning(move |_| Ok(devices.clone()));
        // First call fails, second succeeds
        client
            .expect_update_device()
            .times(1)
            .returning(|_| Err(eyre::eyre!("simulated failure")));
        client
            .expect_update_device()
            .times(1)
            .returning(|_| Ok(Signature::new_unique()));

        let mut out = Vec::new();
        let res = MigrateMulticastCountsCommand { dry_run: false }.execute(&client, &mut out);
        assert!(res.is_ok());
        let output = String::from_utf8(out).unwrap();
        assert!(output.contains("1 device(s) corrected"));
    }

    #[test]
    fn test_migrate_dry_run() {
        let mut client = create_test_client();
        let device_pubkey = Pubkey::new_unique();
        let device = make_device(5, 0); // stale

        let mut users: HashMap<Pubkey, User> = HashMap::new();
        users.insert(
            Pubkey::new_unique(),
            make_multicast_user(device_pubkey, false),
        );
        users.insert(
            Pubkey::new_unique(),
            make_multicast_user(device_pubkey, true),
        );
        let devices: HashMap<Pubkey, Device> = HashMap::from([(device_pubkey, device)]);

        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));
        client
            .expect_list_device()
            .returning(move |_| Ok(devices.clone()));
        client.expect_update_device().times(0); // must NOT be called

        let mut out = Vec::new();
        let res = MigrateMulticastCountsCommand { dry_run: true }.execute(&client, &mut out);
        assert!(res.is_ok());
        let output = String::from_utf8(out).unwrap();
        // Should show what would be corrected
        assert!(output.contains("subscribers"));
        assert!(output.contains("publishers"));
        // Should NOT submit
        assert!(output.contains("[dry-run]"));
    }

    #[test]
    fn test_migrate_unicast_no_op_when_counts_correct() {
        let mut client = create_test_client();
        let device_pubkey = Pubkey::new_unique();
        let device = make_device_unicast(3);

        let mut users: HashMap<Pubkey, User> = HashMap::new();
        for _ in 0..3 {
            users.insert(Pubkey::new_unique(), make_unicast_user(device_pubkey));
        }
        let devices: HashMap<Pubkey, Device> = HashMap::from([(device_pubkey, device)]);

        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));
        client
            .expect_list_device()
            .returning(move |_| Ok(devices.clone()));
        client.expect_update_device().times(0);

        let mut out = Vec::new();
        let res = MigrateUnicastCountsCommand { dry_run: false }.execute(&client, &mut out);
        assert!(res.is_ok());
        assert!(String::from_utf8(out).unwrap().contains("0 device(s)"));
    }

    #[test]
    fn test_migrate_unicast_corrects_stale_counts() {
        let mut client = create_test_client();
        let device_pubkey = Pubkey::new_unique();
        let device = make_device_unicast(5); // stale: stored=5, actual=3

        let mut users: HashMap<Pubkey, User> = HashMap::new();
        for _ in 0..3 {
            users.insert(Pubkey::new_unique(), make_unicast_user(device_pubkey));
        }
        let devices: HashMap<Pubkey, Device> = HashMap::from([(device_pubkey, device)]);

        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));
        client
            .expect_list_device()
            .returning(move |_| Ok(devices.clone()));
        client
            .expect_update_device()
            .times(1)
            .returning(|_| Ok(Signature::new_unique()));

        let mut out = Vec::new();
        let res = MigrateUnicastCountsCommand { dry_run: false }.execute(&client, &mut out);
        assert!(res.is_ok());
        assert!(String::from_utf8(out)
            .unwrap()
            .contains("1 device(s) corrected"));
    }

    #[test]
    fn test_migrate_unicast_failure_on_one_device_does_not_prevent_others() {
        let mut client = create_test_client();
        let device_pk_a = Pubkey::new_unique();
        let device_pk_b = Pubkey::new_unique();

        let mut users: HashMap<Pubkey, User> = HashMap::new();
        users.insert(Pubkey::new_unique(), make_unicast_user(device_pk_a));
        users.insert(Pubkey::new_unique(), make_unicast_user(device_pk_b));

        let devices: HashMap<Pubkey, Device> = HashMap::from([
            (device_pk_a, make_device_unicast(5)),
            (device_pk_b, make_device_unicast(5)),
        ]);

        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));
        client
            .expect_list_device()
            .returning(move |_| Ok(devices.clone()));
        client
            .expect_update_device()
            .times(1)
            .returning(|_| Err(eyre::eyre!("simulated failure")));
        client
            .expect_update_device()
            .times(1)
            .returning(|_| Ok(Signature::new_unique()));

        let mut out = Vec::new();
        let res = MigrateUnicastCountsCommand { dry_run: false }.execute(&client, &mut out);
        assert!(res.is_ok());
        assert!(String::from_utf8(out)
            .unwrap()
            .contains("1 device(s) corrected"));
    }

    #[test]
    fn test_migrate_unicast_dry_run() {
        let mut client = create_test_client();
        let device_pubkey = Pubkey::new_unique();
        let device = make_device_unicast(5); // stale

        let mut users: HashMap<Pubkey, User> = HashMap::new();
        users.insert(Pubkey::new_unique(), make_unicast_user(device_pubkey));
        let devices: HashMap<Pubkey, Device> = HashMap::from([(device_pubkey, device)]);

        client
            .expect_list_user()
            .returning(move |_| Ok(users.clone()));
        client
            .expect_list_device()
            .returning(move |_| Ok(devices.clone()));
        client.expect_update_device().times(0);

        let mut out = Vec::new();
        let res = MigrateUnicastCountsCommand { dry_run: true }.execute(&client, &mut out);
        assert!(res.is_ok());
        let output = String::from_utf8(out).unwrap();
        assert!(output.contains("unicast_users_count"));
        assert!(output.contains("[dry-run]"));
    }
}
