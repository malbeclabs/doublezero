use crate::{doublezerocommand::CliCommand, validators::validate_pubkey_or_code};
use clap::Args;
use doublezero_program_common::{serializer, types::parse_utils::bandwidth_to_string};
use doublezero_sdk::commands::{
    accesspass::list::ListAccessPassCommand, device::list::ListDeviceCommand,
    location::list::ListLocationCommand, multicastgroup::get::GetMulticastGroupCommand,
    tenant::list::ListTenantCommand, user::list::ListUserCommand,
};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr};
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct GetMulticastGroupCliCommand {
    /// MulticastCroup code or Pubkey to query
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub code: String,
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
}

#[derive(Tabled, Serialize)]
struct MulticastGroupDisplay {
    pub account: String,
    pub code: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(skip)]
    pub tenant_pk: Pubkey,
    pub tenant: String,
    pub multicast_ip: String,
    pub max_bandwidth: String,
    pub publisher_count: u32,
    pub subscriber_count: u32,
    pub status: String,
    pub owner: String,
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

#[derive(Tabled, Serialize)]
struct MulticastUserDisplay {
    pub account: String,
    pub multicast_mode: String,
    pub device: String,
    pub location: String,
    pub cyoa_type: String,
    pub client_ip: String,
    pub tunnel_id: u16,
    pub tunnel_net: String,
    pub dz_ip: String,
    pub status: String,
    pub owner: String,
}

#[derive(Serialize)]
struct MulticastGroupOutput {
    #[serde(flatten)]
    pub info: MulticastGroupDisplay,
    pub allowlist: Vec<MulticastAllowlistDisplay>,
    pub users: Vec<MulticastUserDisplay>,
}

impl GetMulticastGroupCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (mgroup_pubkey, mgroup) = client.get_multicastgroup(GetMulticastGroupCommand {
            pubkey_or_code: self.code,
        })?;

        let users = client.list_user(ListUserCommand)?;
        let devices = client.list_device(ListDeviceCommand)?;
        let locations = client.list_location(ListLocationCommand)?;

        let list_accesspass = client.list_accesspass(ListAccessPassCommand {})?;

        let allowlist: Vec<MulticastAllowlistDisplay> = list_accesspass
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
            .collect();

        let user_displays: Vec<MulticastUserDisplay> = users
            .into_iter()
            .filter(|(_, user)| {
                user.publishers.contains(&mgroup_pubkey)
                    || user.subscribers.contains(&mgroup_pubkey)
            })
            .map(|(pubkey, user)| {
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

                MulticastUserDisplay {
                    account: pubkey.to_string(),
                    multicast_mode: mode_text.to_string(),
                    device: device_name,
                    location: location_name,
                    cyoa_type: user.cyoa_type.to_string(),
                    client_ip: user.client_ip.to_string(),
                    tunnel_id: user.tunnel_id,
                    tunnel_net: user.tunnel_net.to_string(),
                    dz_ip: user.dz_ip.to_string(),
                    status: user.status.to_string(),
                    owner: user.owner.to_string(),
                }
            })
            .collect();

        let tenant_str = if mgroup.tenant_pk == Pubkey::default() {
            String::new()
        } else {
            let tenants = client.list_tenant(ListTenantCommand {})?;

            tenants
                .get(&mgroup.tenant_pk)
                .map_or(mgroup.tenant_pk.to_string(), |t| t.code.clone())
        };

        let info = MulticastGroupDisplay {
            account: mgroup_pubkey.to_string(),
            code: mgroup.code,
            tenant_pk: mgroup.tenant_pk,
            tenant: tenant_str,
            multicast_ip: mgroup.multicast_ip.to_string(),
            max_bandwidth: bandwidth_to_string(&mgroup.max_bandwidth),
            publisher_count: mgroup.publisher_count,
            subscriber_count: mgroup.subscriber_count,
            status: mgroup.status.to_string(),
            owner: mgroup.owner.to_string(),
        };

        if self.json {
            let output_data = MulticastGroupOutput {
                info,
                allowlist,
                users: user_displays,
            };
            let json = serde_json::to_string_pretty(&output_data)?;
            writeln!(out, "{json}")?;
        } else {
            let headers = MulticastGroupDisplay::headers();
            let fields = info.fields();
            let max_len = headers.iter().map(|h| h.len()).max().unwrap_or(0);
            for (header, value) in headers.iter().zip(fields.iter()) {
                writeln!(out, " {header:<max_len$} | {value}")?;
            }

            let allowlist_table = Table::new(allowlist)
                .with(Style::psql().remove_horizontals())
                .to_string();
            writeln!(out, "\r\nallowlist:\r\n{allowlist_table}")?;

            let users_table = Table::new(user_displays)
                .with(Style::psql().remove_horizontals())
                .to_string();
            writeln!(out, "\r\nusers:\r\n{users_table}")?;
        }

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
            tenant::list::ListTenantCommand,
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
            unicast_users_count: 0,
            multicast_subscribers_count: 0,
            max_unicast_users: 0,
            max_multicast_subscribers: 0,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
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
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
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

        client
            .expect_list_tenant()
            .with(predicate::eq(ListTenantCommand {}))
            .returning(move |_| Ok(std::collections::HashMap::new()));

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

        // Expected failure
        let mut output = Vec::new();
        let res = GetMulticastGroupCliCommand {
            code: Pubkey::new_unique().to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_err(), "I shouldn't find anything.");

        // Expected success by pubkey (table)
        let mut output = Vec::new();
        let res = GetMulticastGroupCliCommand {
            code: mgroup_pubkey.to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by pubkey");
        let output_str = String::from_utf8(output).unwrap();
        let has_row = |header: &str, value: &str| {
            output_str
                .lines()
                .any(|l| l.contains(header) && l.contains(value))
        };
        assert!(
            has_row("account", &mgroup_pubkey.to_string()),
            "account row should contain pubkey"
        );
        assert!(has_row("code", "test"), "code row should contain value");
        assert!(
            has_row("status", "activated"),
            "status row should contain value"
        );
        assert!(
            output_str.contains("allowlist"),
            "should contain allowlist section"
        );
        assert!(output_str.contains("users"), "should contain users section");

        // Expected success by code (JSON)
        let mut output = Vec::new();
        let res = GetMulticastGroupCliCommand {
            code: "test".to_string(),
            json: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
        let json: serde_json::Value =
            serde_json::from_str(&String::from_utf8(output).unwrap()).unwrap();
        assert_eq!(json["account"].as_str().unwrap(), mgroup_pubkey.to_string());
        assert_eq!(json["code"].as_str().unwrap(), "test");
        assert_eq!(json["status"].as_str().unwrap(), "activated");
        assert!(
            json["allowlist"].is_array(),
            "allowlist should be a JSON array"
        );
        assert!(json["users"].is_array(), "users should be a JSON array");
    }
}
