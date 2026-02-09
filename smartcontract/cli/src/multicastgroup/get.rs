use crate::{doublezerocommand::CliCommand, validators::validate_pubkey_or_code};
use clap::Args;
use doublezero_program_common::{serializer, types::parse_utils::bandwidth_to_string};
use doublezero_sdk::commands::{
    accesspass::list::ListAccessPassCommand, device::list::ListDeviceCommand,
    location::list::ListLocationCommand, multicastgroup::get::GetMulticastGroupCommand,
    user::list::ListUserCommand,
};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr};
use tabled::{builder::Builder, settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct GetMulticastGroupCliCommand {
    /// MulticastCroup code or Pubkey to query
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub code: String,
}

#[derive(Tabled, Serialize)]
pub struct MulticastAllowlistDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub mode: String,
    pub client_ip: Ipv4Addr,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub user_payer: Pubkey,
}

impl GetMulticastGroupCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (mgroup_pubkey, mgroup) = client.get_multicastgroup(GetMulticastGroupCommand {
            pubkey_or_code: self.code,
        })?;

        let users = client.list_user(ListUserCommand)?;
        let devices = client.list_device(ListDeviceCommand)?;
        let locations = client.list_location(ListLocationCommand)?;

        // Write the multicast group details first
        writeln!(out,
        "account: {}\r\ncode: {}\r\nmulticast_ip: {}\r\nmax_bandwidth: {}\r\nstatus: {}\r\nowner: {}",
        mgroup_pubkey,
        mgroup.code,
        &mgroup.multicast_ip,
        bandwidth_to_string(&mgroup.max_bandwidth),
        mgroup.status,
        mgroup.owner
        )?;

        let list_accesspass = client.list_accesspass(ListAccessPassCommand {})?;

        let mga_displays = list_accesspass
            .into_iter()
            .filter(|(_, accesspass)| {
                accesspass.mgroup_sub_allowlist.contains(&mgroup_pubkey)
                    || accesspass.mgroup_pub_allowlist.contains(&mgroup_pubkey)
            })
            .map(|(_, accesspass)| MulticastAllowlistDisplay {
                account: mgroup_pubkey,
                mode: if accesspass.mgroup_pub_allowlist.contains(&mgroup_pubkey) {
                    if accesspass.mgroup_sub_allowlist.contains(&mgroup_pubkey) {
                        "P+S".to_string()
                    } else {
                        "P".to_string()
                    }
                } else {
                    "S".to_string()
                },
                client_ip: accesspass.client_ip,
                user_payer: accesspass.user_payer,
            })
            .collect::<Vec<_>>();

        let table = Table::new(mga_displays)
            .with(Style::psql().remove_horizontals())
            .to_string();

        writeln!(out, "\r\nallowlist:\r\n{table}")?;

        let mut builder = Builder::default();
        builder.push_record([
            "account",
            "multicast_mode",
            "device",
            "location",
            "cyoa_type",
            "client_ip",
            "tunnel_id",
            "tunnel_net",
            "dz_ip",
            "status",
            "owner",
        ]);

        for (pubkey, user) in users.into_iter().filter(|(_, user)| {
            user.publishers.contains(&mgroup_pubkey) || user.subscribers.contains(&mgroup_pubkey)
        }) {
            let device = devices.get(&user.device_pk);
            let location = match device {
                Some(device) => locations.get(&device.location_pk),
                None => None,
            };

            let device_name = match device {
                Some(device) => device.code.clone(),
                None => user.device_pk.to_string(),
            };
            let location_name = match device {
                Some(device) => match location {
                    Some(location) => location.name.clone(),
                    None => device.location_pk.to_string(),
                },
                None => "".to_string(),
            };
            let mode_text = if user.publishers.contains(&mgroup_pubkey) {
                if !user.subscribers.contains(&mgroup_pubkey) {
                    "P"
                } else {
                    "PS"
                }
            } else if user.subscribers.contains(&mgroup_pubkey) {
                "S"
            } else {
                "X"
            };

            builder.push_record([
                &pubkey.to_string(),
                mode_text,
                &device_name,
                &location_name,
                &user.cyoa_type.to_string(),
                user.client_ip.to_string().as_str(),
                &user.tunnel_id.to_string(),
                user.tunnel_net.to_string().as_str(),
                user.dz_ip.to_string().as_str(),
                &user.status.to_string(),
                &user.owner.to_string(),
            ]);
        }

        let table = builder
            .build()
            .with(Style::psql().remove_horizontals())
            .to_string();

        writeln!(out, "\r\nusers:\r\n{table}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand, multicastgroup::get::GetMulticastGroupCliCommand,
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::{
            device::{get::GetDeviceCommand, list::ListDeviceCommand},
            location::list::ListLocationCommand,
            multicastgroup::get::GetMulticastGroupCommand,
        },
        get_multicastgroup_pda, AccountType, Device, DeviceStatus, GetLocationCommand, Location,
        LocationStatus, MulticastGroup, MulticastGroupStatus, User, UserCYOA, UserStatus, UserType,
    };
    use doublezero_serviceability::state::accesspass::{
        AccessPass, AccessPassStatus, AccessPassType,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_multicastgroup_get() {
        let mut client = create_test_client();

        let location_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1");
        let location = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            name: "test_location".to_string(),
            code: "test_location".to_string(),
            owner: Pubkey::default(),
            lat: 1.0,
            lng: 1.0,
            loc_id: 0,
            status: LocationStatus::Activated,
            country: "US".to_string(),
        };

        let cloned_location = location.clone();
        client
            .expect_get_location()
            .with(predicate::eq(GetLocationCommand {
                pubkey_or_code: location_pubkey.to_string(),
            }))
            .returning(move |_| Ok((location_pubkey, cloned_location.clone())));
        let cloned_location = location.clone();
        client
            .expect_list_location()
            .with(predicate::eq(ListLocationCommand))
            .returning(move |_| {
                let mut locations = std::collections::HashMap::new();
                locations.insert(location_pubkey, cloned_location.clone());
                Ok(locations)
            });

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let device_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1");
        let device = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test_device".to_string(),
            device_type: doublezero_sdk::DeviceType::Hybrid,
            contributor_pk,
            location_pk: Pubkey::default(),
            status: DeviceStatus::Activated,
            owner: device_pubkey,
            exchange_pk: location_pubkey,
            public_ip: [10, 0, 0, 1].into(),
            dz_prefixes: "10.0.0.0/32".parse().unwrap(),
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
        };

        let cloned_device = device.clone();
        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device_pubkey.to_string(),
            }))
            .returning(move |_| Ok((device_pubkey, cloned_device.clone())));

        let cloned_device = device.clone();
        client
            .expect_list_device()
            .with(predicate::eq(ListDeviceCommand))
            .returning(move |_| {
                let mut devices = std::collections::HashMap::new();
                devices.insert(device_pubkey, cloned_device.clone());
                Ok(devices)
            });

        let (mgroup_pubkey, _bump_seed) = get_multicastgroup_pda(&client.get_program_id(), 1);

        let user1_pk = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1");
        let user1 = User {
            account_type: AccountType::User,
            index: 1,
            bump_seed: 255,
            user_type: UserType::Multicast,
            tenant_pk: Pubkey::default(),
            device_pk: Pubkey::default(),
            client_ip: [192, 168, 1, 1].into(),
            dz_ip: [10, 0, 0, 2].into(),
            tunnel_id: 12345,
            tunnel_net: "10.0.0.0/32".parse().unwrap(),
            cyoa_type: UserCYOA::GREOverDIA,
            publishers: vec![mgroup_pubkey],
            subscribers: vec![],
            status: UserStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1"),
            validator_pubkey: Pubkey::default(),
        };

        let multicastgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            tenant_pk: Pubkey::default(),
            multicast_ip: [10, 0, 0, 1].into(),
            max_bandwidth: 1000000000,
            status: MulticastGroupStatus::Activated,
            owner: mgroup_pubkey,
            publisher_count: 5,
            subscriber_count: 10,
        };

        client.expect_list_user().returning(move |_| {
            let mut users = std::collections::HashMap::new();
            users.insert(user1_pk, user1.clone());
            Ok(users)
        });

        client.expect_list_accesspass().returning(move |_| {
            let mut accesspasses = std::collections::HashMap::new();
            accesspasses.insert(
                Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1"),
                AccessPass {
                    account_type: AccountType::AccessPass,
                    bump_seed: 255,
                    accesspass_type: AccessPassType::Prepaid,
                    last_access_epoch: u64::MAX,
                    connection_count: 0,
                    user_payer: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1"),
                    client_ip: [192, 168, 1, 1].into(),
                    mgroup_pub_allowlist: vec![mgroup_pubkey],
                    mgroup_sub_allowlist: vec![mgroup_pubkey],
                    owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1"),
                    tenant_allowlist: vec![],
                    status: AccessPassStatus::Requested,
                    flags: 0,
                },
            );
            Ok(accesspasses)
        });

        let multicastgroup2 = multicastgroup.clone();
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: mgroup_pubkey.to_string(),
            }))
            .returning(move |_| Ok((mgroup_pubkey, multicastgroup.clone())));
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: "test".to_string(),
            }))
            .returning(move |_| Ok((mgroup_pubkey, multicastgroup2.clone())));
        client
            .expect_get_multicastgroup()
            .returning(move |_| Err(eyre::eyre!("not found")));
        /*****************************************************************************************************/
        // Expected failure
        let mut output = Vec::new();
        let res = GetMulticastGroupCliCommand {
            code: Pubkey::new_unique().to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_err(), "I shouldn't find anything.");

        // Expected success
        let mut output = Vec::new();
        let res = GetMulticastGroupCliCommand {
            code: mgroup_pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by pubkey");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: G4DjGHreV54t5yeNuSHi5iVcT5Qkykuj43pWWdSsP3dj\r\ncode: test\r\nmulticast_ip: 10.0.0.1\r\nmax_bandwidth: 1Gbps\r\nstatus: activated\r\nowner: G4DjGHreV54t5yeNuSHi5iVcT5Qkykuj43pWWdSsP3dj\n\r\nallowlist:\r\n account                                      | mode | client_ip   | user_payer                                \n G4DjGHreV54t5yeNuSHi5iVcT5Qkykuj43pWWdSsP3dj | P+S  | 192.168.1.1 | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1 \n\r\nusers:\r\n account                                   | multicast_mode | device                           | location | cyoa_type  | client_ip   | tunnel_id | tunnel_net  | dz_ip    | status    | owner                                     \n 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1 | P              | 11111111111111111111111111111111 |          | GREOverDIA | 192.168.1.1 | 12345     | 10.0.0.0/32 | 10.0.0.2 | activated | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1 \n");

        // Expected success
        let mut output = Vec::new();
        let res = GetMulticastGroupCliCommand {
            code: "test".to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: G4DjGHreV54t5yeNuSHi5iVcT5Qkykuj43pWWdSsP3dj\r\ncode: test\r\nmulticast_ip: 10.0.0.1\r\nmax_bandwidth: 1Gbps\r\nstatus: activated\r\nowner: G4DjGHreV54t5yeNuSHi5iVcT5Qkykuj43pWWdSsP3dj\n\r\nallowlist:\r\n account                                      | mode | client_ip   | user_payer                                \n G4DjGHreV54t5yeNuSHi5iVcT5Qkykuj43pWWdSsP3dj | P+S  | 192.168.1.1 | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1 \n\r\nusers:\r\n account                                   | multicast_mode | device                           | location | cyoa_type  | client_ip   | tunnel_id | tunnel_net  | dz_ip    | status    | owner                                     \n 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1 | P              | 11111111111111111111111111111111 |          | GREOverDIA | 192.168.1.1 | 12345     | 10.0.0.0/32 | 10.0.0.2 | activated | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1 \n");
    }
}
