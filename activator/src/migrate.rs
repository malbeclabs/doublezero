use doublezero_sdk::{
    commands::device::update::UpdateDeviceCommand, AccountData, AccountType, DoubleZeroClient,
    UserStatus, UserType,
};
use log::{info, warn};
use solana_sdk::pubkey::Pubkey;
use std::collections::HashMap;

/// One-time migration: scan all User accounts and correct per-device multicast
/// publisher/subscriber counts. Run at activator startup.
///
/// Background: before the publisher/subscriber split, `multicast_users_count`
/// tracked all multicast users together. After the rename to
/// `multicast_subscribers_count`, this field may contain inflated counts for
/// existing deployments because publisher users were also included. Meanwhile
/// `multicast_publishers_count` is 0 for all pre-upgrade devices. This function
/// computes the true counts from User account state and writes corrections.
pub fn migrate_multicast_counts(client: &dyn DoubleZeroClient) -> eyre::Result<()> {
    info!("migrate_multicast_counts: scanning user accounts...");

    // Tally multicast publishers and subscribers per device pubkey.
    let users = client.gets(AccountType::User)?;
    let mut per_device: HashMap<Pubkey, (u16, u16)> = HashMap::new(); // (publishers, subscribers)
    for account in users.values() {
        if let AccountData::User(user) = account {
            // Exclude terminal/invalid states that will never trigger a counter decrement.
            // Note: SuspendedDeprecated is intentionally included — legacy accounts in this
            // state still hold a device slot and must be counted.
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
    }

    let devices = client.gets(AccountType::Device)?;
    let mut corrections = 0u32;

    for (device_pubkey, account) in &devices {
        if let AccountData::Device(device) = account {
            let (actual_pub, actual_sub) = per_device.get(device_pubkey).copied().unwrap_or((0, 0));

            if device.multicast_publishers_count == actual_pub
                && device.multicast_subscribers_count == actual_sub
            {
                continue;
            }

            info!(
                "migrate_multicast_counts: device {} ({}): \
                 subscribers {} -> {}, publishers {} -> {}",
                device.code,
                device_pubkey,
                device.multicast_subscribers_count,
                actual_sub,
                device.multicast_publishers_count,
                actual_pub,
            );

            let result = UpdateDeviceCommand {
                pubkey: *device_pubkey,
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
                max_multicast_subscribers: None,
                max_multicast_publishers: None,
                multicast_subscribers_count: Some(actual_sub),
                multicast_publishers_count: Some(actual_pub),
            }
            .execute(client);

            match result {
                Ok(sig) => {
                    corrections += 1;
                    info!(
                        "migrate_multicast_counts: corrected device {} ({}): {sig}",
                        device.code, device_pubkey
                    );
                }
                Err(e) => {
                    // Log but don't abort; attempt remaining devices
                    warn!(
                        "migrate_multicast_counts: failed to correct device {} ({}): {e}",
                        device.code, device_pubkey
                    );
                }
            }
        }
    }

    info!("migrate_multicast_counts: done, {corrections} device(s) corrected");
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_sdk::{
        AccountData, AccountType, Device, DeviceStatus, DeviceType, User, UserCYOA, UserStatus,
        UserType,
    };
    use doublezero_serviceability::state::device::{DeviceDesiredStatus, DeviceHealth};
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::collections::HashMap;

    fn make_device(multicast_subscribers_count: u16, multicast_publishers_count: u16) -> Device {
        Device {
            account_type: AccountType::Device,
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
            multicast_subscribers_count,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count,
            max_multicast_publishers: 0,
        }
    }

    fn make_multicast_user(device_pk: Pubkey, is_publisher: bool) -> User {
        User {
            account_type: AccountType::User,
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
        }
    }

    #[test]
    fn test_migrate_no_op_when_counts_correct() {
        let mut client = create_test_client();
        let device_pubkey = Pubkey::new_unique();

        // Device already has correct counts: 3 subscribers, 2 publishers
        let device = make_device(3, 2);

        // 3 subscriber users + 2 publisher users for this device
        let mut users: HashMap<Pubkey, AccountData> = HashMap::new();
        for _ in 0..3 {
            users.insert(
                Pubkey::new_unique(),
                AccountData::User(make_multicast_user(device_pubkey, false)),
            );
        }
        for _ in 0..2 {
            users.insert(
                Pubkey::new_unique(),
                AccountData::User(make_multicast_user(device_pubkey, true)),
            );
        }

        let devices: HashMap<Pubkey, AccountData> =
            HashMap::from([(device_pubkey, AccountData::Device(device))]);

        client
            .expect_gets()
            .with(predicate::eq(AccountType::User))
            .returning(move |_| Ok(users.clone()));

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Device))
            .returning(move |_| Ok(devices.clone()));

        // execute_transaction must NOT be called
        client.expect_execute_transaction().times(0);

        let result = migrate_multicast_counts(&client);
        assert!(result.is_ok());
    }

    #[test]
    fn test_migrate_corrects_stale_counts() {
        let mut client = create_test_client();
        let device_pubkey = Pubkey::new_unique();

        // Device has stale counts: subscribers=5, publishers=0
        let device = make_device(5, 0);

        // Actual state: 3 subscribers + 2 publishers
        let mut users: HashMap<Pubkey, AccountData> = HashMap::new();
        for _ in 0..3 {
            users.insert(
                Pubkey::new_unique(),
                AccountData::User(make_multicast_user(device_pubkey, false)),
            );
        }
        for _ in 0..2 {
            users.insert(
                Pubkey::new_unique(),
                AccountData::User(make_multicast_user(device_pubkey, true)),
            );
        }

        let device_clone = device.clone();
        let devices: HashMap<Pubkey, AccountData> =
            HashMap::from([(device_pubkey, AccountData::Device(device))]);

        client
            .expect_gets()
            .with(predicate::eq(AccountType::User))
            .returning(move |_| Ok(users.clone()));

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Device))
            .returning(move |_| Ok(devices.clone()));

        // UpdateDeviceCommand.execute calls client.get(device_pubkey) via GetDeviceCommand
        client
            .expect_get()
            .with(predicate::eq(device_pubkey))
            .returning(move |_| Ok(AccountData::Device(device_clone.clone())));

        // execute_transaction must be called exactly once
        client
            .expect_execute_transaction()
            .times(1)
            .returning(|_, _| Ok(Signature::new_unique()));

        let result = migrate_multicast_counts(&client);
        assert!(result.is_ok());
    }

    #[test]
    fn test_migrate_failure_on_one_device_does_not_prevent_others() {
        let mut client = create_test_client();
        let device_pubkey_a = Pubkey::new_unique();
        let device_pubkey_b = Pubkey::new_unique();

        // Both devices have stale counts: subscribers=5, publishers=0
        let device_a = make_device(5, 0);
        let device_b = make_device(5, 0);

        // Each device has 1 subscriber and 1 publisher (actual counts differ from stale)
        let mut users: HashMap<Pubkey, AccountData> = HashMap::new();
        users.insert(
            Pubkey::new_unique(),
            AccountData::User(make_multicast_user(device_pubkey_a, false)),
        );
        users.insert(
            Pubkey::new_unique(),
            AccountData::User(make_multicast_user(device_pubkey_a, true)),
        );
        users.insert(
            Pubkey::new_unique(),
            AccountData::User(make_multicast_user(device_pubkey_b, false)),
        );
        users.insert(
            Pubkey::new_unique(),
            AccountData::User(make_multicast_user(device_pubkey_b, true)),
        );

        let device_a_clone = device_a.clone();
        let device_b_clone = device_b.clone();
        let devices: HashMap<Pubkey, AccountData> = HashMap::from([
            (device_pubkey_a, AccountData::Device(device_a)),
            (device_pubkey_b, AccountData::Device(device_b)),
        ]);

        client
            .expect_gets()
            .with(predicate::eq(AccountType::User))
            .returning(move |_| Ok(users.clone()));

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Device))
            .returning(move |_| Ok(devices.clone()));

        // UpdateDeviceCommand.execute calls client.get(device_pubkey) for each device
        client
            .expect_get()
            .with(predicate::eq(device_pubkey_a))
            .returning(move |_| Ok(AccountData::Device(device_a_clone.clone())));

        client
            .expect_get()
            .with(predicate::eq(device_pubkey_b))
            .returning(move |_| Ok(AccountData::Device(device_b_clone.clone())));

        // execute_transaction is called twice: fails for first device, succeeds for second
        client
            .expect_execute_transaction()
            .times(1)
            .returning(|_, _| Err(eyre::eyre!("simulated transaction failure")));

        client
            .expect_execute_transaction()
            .times(1)
            .returning(|_, _| Ok(Signature::new_unique()));

        // Overall result is Ok despite per-device failure
        let result = migrate_multicast_counts(&client);
        assert!(result.is_ok());
    }
}
