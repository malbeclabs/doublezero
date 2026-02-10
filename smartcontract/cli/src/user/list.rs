use crate::{doublezerocommand::CliCommand, helpers::parse_pubkey};
use clap::Args;
use doublezero_program_common::{serializer, types::NetworkV4};
use doublezero_sdk::{
    commands::{
        accesspass::list::ListAccessPassCommand, device::list::ListDeviceCommand,
        location::list::ListLocationCommand, multicastgroup::list::ListMulticastGroupCommand,
        tenant::list::ListTenantCommand, user::list::ListUserCommand,
    },
    read_doublezero_config, MulticastGroup, User, UserCYOA, UserStatus, UserType,
};
use doublezero_serviceability::pda::get_accesspass_pda;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, io::Write, net::Ipv4Addr};
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct ListUserCliCommand {
    /// Filter by prepaid access passes
    #[arg(long, default_value_t = false)]
    pub prepaid: bool,
    /// Filter by Solana validator access passes
    #[arg(long, default_value_t = false)]
    pub solana_validator: bool,
    /// Filter by Solana identity public key
    #[arg(long, value_delimiter = ',', value_name = "SOLANA_IDENTITY,...")]
    pub solana_identity: Option<Vec<Pubkey>>,
    /// Filter by device code
    #[arg(long, value_delimiter = ',', value_name = "DEVICE_CODE,...")]
    pub device: Option<Vec<String>>,
    /// Filter by location code
    #[arg(long, value_delimiter = ',', value_name = "LOCATION_CODE_OR_NAME,...")]
    pub location: Option<Vec<String>>,
    /// Filter by client IP address
    #[arg(long, value_delimiter = ',', value_name = "CLIENT_IP,...")]
    pub client_ip: Option<Vec<Ipv4Addr>>,
    /// Filter by owner public key
    #[arg(long, value_delimiter = ',', value_name = "OWNER_PUBLIC_KEY,...")]
    pub owner: Option<Vec<Pubkey>>,
    /// Filter by user type
    #[arg(long, value_delimiter = ',', value_name = "USER_TYPE,...")]
    pub user_type: Option<Vec<String>>,
    /// Filter by CYOA type
    #[arg(long, value_delimiter = ',', value_name = "CYOA_TYPE,...")]
    pub cyoa_type: Option<Vec<String>>,
    /// Filter by DoubleZero IP address
    #[arg(long, value_delimiter = ',', value_name = "DZ_IP,...")]
    pub dz_ip: Option<Vec<Ipv4Addr>>,
    /// Filter by tunnel ID
    #[arg(long, value_delimiter = ',', value_name = "TUNNEL_ID,...")]
    pub tunnel_id: Option<Vec<u16>>,
    /// Filter by status
    #[arg(long, value_delimiter = ',', value_name = "STATUS,...")]
    pub status: Option<Vec<String>>,
    /// Filter by multicast group (as publisher or subscriber)
    #[arg(long, value_delimiter = ',', value_name = "MULTICAST_GROUP,...")]
    pub multicast_group: Option<Vec<String>>,
    /// Filter by tenant (pubkey or code)
    #[arg(
        short = 't',
        long,
        conflicts_with = "all_tenants",
        value_delimiter = ',',
        value_name = "TENANT_CODE_OR_PUBKEY,..."
    )]
    pub tenant: Option<Vec<String>>,
    /// Ignore the default tenant from config
    #[arg(long, conflicts_with = "tenant")]
    pub all_tenants: bool,
    /// Output as pretty JSON.
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON.
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct UserDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub tenant: String,
    pub user_type: UserType,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(skip)]
    pub device_pk: Pubkey,
    #[tabled(rename = "groups")]
    pub multicast: String,
    #[tabled(skip)]
    #[serde(serialize_with = "serializer::serialize_pubkeylist_as_string")]
    pub publishers: Vec<Pubkey>,
    #[tabled(skip)]
    #[serde(serialize_with = "serializer::serialize_pubkeylist_as_string")]
    pub subscribers: Vec<Pubkey>,
    #[tabled(rename = "device")]
    pub device_name: String,
    #[tabled(skip)]
    pub location_code: String,
    #[tabled(rename = "location")]
    pub location_name: String,
    pub cyoa_type: UserCYOA,
    pub client_ip: Ipv4Addr,
    pub dz_ip: Ipv4Addr,
    pub accesspass: String,
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
    pub status: UserStatus,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListUserCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let devices = client.list_device(ListDeviceCommand)?;
        let locations = client.list_location(ListLocationCommand)?;
        let mgroups = client.list_multicastgroup(ListMulticastGroupCommand)?;
        let accesspasses = client.list_accesspass(ListAccessPassCommand {})?;
        let tenants = client.list_tenant(ListTenantCommand {})?;
        let binding = client.list_user(ListUserCommand)?;

        let mut users = binding
            .iter()
            .map(|(pk, user)| {
                let (accesspass_pk, _) = get_accesspass_pda(
                    &client.get_program_id(),
                    &Ipv4Addr::UNSPECIFIED,
                    &user.owner,
                );

                match accesspasses.get(&accesspass_pk) {
                    Some(accesspass) => (pk, user.clone(), Some(accesspass.clone())),
                    None => {
                        let (accesspass_pk, _) = get_accesspass_pda(
                            &client.get_program_id(),
                            &user.client_ip,
                            &user.owner,
                        );
                        let accesspass = accesspasses.get(&accesspass_pk);

                        (pk, user.clone(), accesspass.cloned())
                    }
                }
            })
            .collect::<Vec<_>>();

        // Filter users by access pass type
        if self.prepaid {
            users.retain(|(_, _, accesspass)| {
                if let Some(accesspass) = accesspass {
                    accesspass.accesspass_type
                        == doublezero_serviceability::state::accesspass::AccessPassType::Prepaid
                } else {
                    false
                }
            });
        }
        // Filter users by access pass type
        if self.solana_validator {
            users.retain(|(_, _, accesspass)| {
                if let Some(accesspass) = accesspass {
                    matches!(
                        accesspass.accesspass_type,
                        doublezero_serviceability::state::accesspass::AccessPassType::SolanaValidator(_)
                    )
                } else {
                    false
                }
            });
        }
        if let Some(solana_identity_vec) = self.solana_identity {
            users.retain(|(_, _, accesspass)| {
                if let Some(accesspass) = accesspass {
                    if let doublezero_serviceability::state::accesspass::AccessPassType::SolanaValidator(pk) = &accesspass.accesspass_type {
                        solana_identity_vec.contains(pk)
                    } else {
                        false
                    }
                } else {
                    false
                }
            });
        }

        if let Some(ref owner_vec) = self.owner {
            users.retain(|(_, user, _)| owner_vec.contains(&user.owner));
        }
        if let Some(ref client_ips) = self.client_ip {
            users.retain(|(_, user, _)| client_ips.contains(&user.client_ip));
        }

        if let Some(ref device_code_vec) = self.device {
            users.retain(|(_, user, _)| {
                if let Some(device) = devices.get(&user.device_pk) {
                    device_code_vec.contains(&device.code)
                } else {
                    false
                }
            });
        }

        if let Some(ref location_code_or_name_vec) = self.location {
            users.retain(|(_, user, _)| {
                if let Some(device) = devices.get(&user.device_pk) {
                    if let Some(location) = locations.get(&device.location_pk) {
                        location_code_or_name_vec.contains(&location.code)
                            || location_code_or_name_vec.contains(&location.name)
                    } else {
                        false
                    }
                } else {
                    false
                }
            });
        }

        if let Some(ref user_type_vec) = self.user_type {
            users.retain(|(_, user, _)| {
                user_type_vec
                    .iter()
                    .any(|ut| ut.to_lowercase() == user.user_type.to_string().to_lowercase())
            });
        }

        if let Some(ref cyoa_type_vec) = self.cyoa_type {
            users.retain(|(_, user, _)| {
                cyoa_type_vec
                    .iter()
                    .any(|ct| ct.to_lowercase() == user.cyoa_type.to_string().to_lowercase())
            });
        }

        if let Some(ref dz_ips) = self.dz_ip {
            users.retain(|(_, user, _)| dz_ips.contains(&user.dz_ip));
        }

        if let Some(ref tunnel_ids) = self.tunnel_id {
            users.retain(|(_, user, _)| tunnel_ids.contains(&user.tunnel_id));
        }

        if let Some(ref status_vec) = self.status {
            users.retain(|(_, user, _)| {
                status_vec
                    .iter()
                    .any(|s| s.to_lowercase() == user.status.to_string().to_lowercase())
            });
        }

        if let Some(ref mgroup_vec) = self.multicast_group {
            users.retain(|(_, user, _)| {
                let user_groups = user.get_multicast_groups();
                mgroup_vec.iter().any(|mg_filter| {
                    user_groups.iter().any(|user_mg_pk| {
                        // Check if matches by pubkey string
                        if mg_filter == &user_mg_pk.to_string() {
                            return true;
                        }
                        // Check if matches by multicast group code
                        if let Some(mgroup) = mgroups.get(user_mg_pk) {
                            if mg_filter == &mgroup.code {
                                return true;
                            }
                        }
                        false
                    })
                })
            });
        }

        let tenant = if self.all_tenants {
            None
        } else {
            self.tenant.or_else(|| {
                read_doublezero_config()
                    .ok()
                    .and_then(|(_, cfg)| cfg.tenant)
                    .map(|t| vec![t])
            })
        };

        if let Some(ref tenant_vec) = tenant {
            users.retain(|(_, user, _)| {
                tenant_vec.iter().any(|tenant_filter| {
                    // Check if matches by pubkey
                    if let Some(pk) = parse_pubkey(tenant_filter) {
                        return user.tenant_pk == pk;
                    }
                    // Check if matches by tenant code
                    if let Some(tenant) = tenants.get(&user.tenant_pk) {
                        return tenant.code.eq_ignore_ascii_case(tenant_filter);
                    }
                    false
                })
            });
        }

        let mut users_displays: Vec<UserDisplay> = users
            .into_iter()
            .map(|(pubkey, user, accesspass)| {
                let device = devices.get(&user.device_pk);
                let location = match device {
                    Some(device) => locations.get(&device.location_pk),
                    None => None,
                };

                let device_name = match device {
                    Some(device) => device.code.clone(),
                    None => user.device_pk.to_string(),
                };
                let location_code = match device {
                    Some(device) => match location {
                        Some(location) => location.code.clone(),
                        None => device.location_pk.to_string(),
                    },
                    None => "".to_string(),
                };
                let location_name = match device {
                    Some(device) => match location {
                        Some(location) => location.name.clone(),
                        None => device.location_pk.to_string(),
                    },
                    None => "".to_string(),
                };

                let tenant_name = tenants
                    .get(&user.tenant_pk)
                    .map_or_else(|| user.tenant_pk.to_string(), |t| t.code.clone());

                UserDisplay {
                    account: *pubkey,
                    tenant: tenant_name,
                    user_type: user.user_type,
                    device_pk: user.device_pk,
                    multicast: format_multicast_group_names(&user, &mgroups),
                    publishers: user.publishers,
                    subscribers: user.subscribers,
                    device_name,
                    location_code,
                    location_name,
                    cyoa_type: user.cyoa_type,
                    client_ip: user.client_ip,
                    dz_ip: user.dz_ip,
                    accesspass: if let Some(accesspass) = accesspass {
                        accesspass.to_string()
                    } else {
                        "none".to_string()
                    },
                    tunnel_id: user.tunnel_id,
                    tunnel_net: user.tunnel_net,
                    status: user.status,
                    owner: user.owner,
                }
            })
            .collect();

        users_displays.sort_by(|a, b| {
            a.device_name
                .cmp(&b.device_name)
                .then(a.tunnel_id.cmp(&b.tunnel_id))
        });

        let res = if self.json {
            serde_json::to_string_pretty(&users_displays)?
        } else if self.json_compact {
            serde_json::to_string(&users_displays)?
        } else {
            Table::new(users_displays)
                .with(Style::psql().remove_horizontals())
                .to_string()
        };

        writeln!(out, "{res}")?;

        Ok(())
    }
}

pub fn format_multicast_group_names(
    user: &User,
    mgroups: &HashMap<Pubkey, MulticastGroup>,
) -> String {
    user.get_multicast_groups()
        .iter()
        .map(|pk| {
            let name = mgroups
                .get(pk)
                .map_or_else(|| pk.to_string(), |group| group.code.clone());

            let mut result = name;
            if user.publishers.contains(pk) {
                result.insert_str(0, "P:");
            }
            if user.subscribers.contains(pk) {
                result.insert_str(0, "S:");
            }
            result
        })
        .collect::<Vec<_>>()
        .join(",")
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{
        doublezerocommand::CliCommand,
        tests::utils::create_test_client,
        user::list::{
            ListUserCliCommand, UserCYOA, UserCYOA::GREOverDIA, UserStatus, UserStatus::Activated,
            UserType::IBRL,
        },
    };
    use doublezero_sdk::{
        AccountType, Device, DeviceStatus, DeviceType, Exchange, ExchangeStatus, Location,
        LocationStatus, MulticastGroup, MulticastGroupStatus, Tenant, User, UserType,
    };
    use doublezero_serviceability::{
        pda::get_accesspass_pda,
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType},
            tenant::TenantPaymentStatus,
        },
    };
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_user_list() {
        let mut client = create_test_client();

        let user1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let user2_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");

        let location1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1");
        let location1 = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "location1_code".to_string(),
            name: "location1_name".to_string(),
            country: "location1_country".to_string(),
            lat: 15.0,
            lng: 15.0,
            loc_id: 6,
            status: LocationStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1"),
        };
        let location2_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo2");
        let location2 = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "location2_code".to_string(),
            name: "location2_name".to_string(),
            country: "location2_country".to_string(),
            lat: 15.0,
            lng: 15.0,
            loc_id: 6,
            status: LocationStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo2"),
        };
        let exchange1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo3");
        let exchange1 = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "exchange1_code".to_string(),
            name: "exchange1_name".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 15.0,
            lng: 15.0,
            bgp_community: 6,
            unused: 0,
            status: ExchangeStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo3"),
        };
        let exchange2_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo4");
        let exchange2 = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "exchange2_code".to_string(),
            name: "exchange2_name".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            lat: 15.0,
            lng: 15.0,
            bgp_community: 6,
            unused: 0,
            status: ExchangeStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo4"),
        };

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let device1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "device1_code".to_string(),
            contributor_pk,
            location_pk: location1_pubkey,
            exchange_pk: exchange1_pubkey,
            device_type: DeviceType::Hybrid,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
        };
        let device2_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo8");
        let device2 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "device2_code".to_string(),
            contributor_pk,
            location_pk: location2_pubkey,
            exchange_pk: exchange2_pubkey,
            device_type: DeviceType::Hybrid,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo8"),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
        };
        let mgroup1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo8");
        let mgroup1 = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 2,
            tenant_pk: Pubkey::default(),
            code: "m_code".to_string(),
            multicast_ip: [1, 2, 3, 4].into(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
            publisher_count: 0,
            subscriber_count: 0,
        };

        client.expect_list_location().returning(move |_| {
            let mut locations = HashMap::new();
            locations.insert(location1_pubkey, location1.clone());
            locations.insert(location2_pubkey, location2.clone());
            Ok(locations)
        });

        client.expect_list_exchange().returning(move |_| {
            let mut exchanges = HashMap::new();
            exchanges.insert(exchange1_pubkey, exchange1.clone());
            exchanges.insert(exchange2_pubkey, exchange2.clone());
            Ok(exchanges)
        });

        client.expect_list_device().returning(move |_| {
            let mut devices = HashMap::new();
            devices.insert(device1_pubkey, device1.clone());
            devices.insert(device2_pubkey, device2.clone());
            Ok(devices)
        });

        client.expect_list_multicastgroup().returning(move |_| {
            let mut mgroups = HashMap::new();
            mgroups.insert(mgroup1_pubkey, mgroup1.clone());
            Ok(mgroups)
        });

        let user1 = User {
            account_type: AccountType::User,
            index: 1,
            bump_seed: 2,
            owner: user1_pubkey,
            user_type: IBRL,
            tenant_pk: Pubkey::default(),
            device_pk: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 4].into(),
            dz_ip: [2, 3, 4, 5].into(),
            tunnel_id: 500,
            tunnel_net: "1.2.3.5/32".parse().unwrap(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        let (accesspass1_pubkey, _) =
            get_accesspass_pda(&client.get_program_id(), &user1.client_ip, &user1.owner);
        let accesspass1 = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 255,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user1.client_ip,
            user_payer: user1.owner,
            last_access_epoch: 10,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            owner: client.get_payer(),
            flags: 0,
        };

        let user2 = User {
            account_type: AccountType::User,
            index: 2,
            bump_seed: 3,
            owner: user2_pubkey,
            user_type: UserType::Multicast,
            tenant_pk: Pubkey::default(),
            device_pk: device1_pubkey,
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 4].into(),
            dz_ip: [2, 3, 4, 5].into(),
            tunnel_id: 500,
            tunnel_net: "1.2.3.5/32".parse().unwrap(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![mgroup1_pubkey],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        let (accesspass2_pubkey, _) =
            get_accesspass_pda(&client.get_program_id(), &user2.client_ip, &user2.owner);
        let accesspass2 = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 255,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user2.client_ip,
            user_payer: user2.owner,
            last_access_epoch: 10,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![mgroup1_pubkey],
            tenant_allowlist: vec![],
            owner: client.get_payer(),
            flags: 0,
        };

        client.expect_list_user().returning(move |_| {
            let mut users = HashMap::new();
            users.insert(user1_pubkey, user1.clone());
            users.insert(user2_pubkey, user2.clone());
            Ok(users)
        });

        client.expect_list_accesspass().returning(move |_| {
            let mut accesspasses = HashMap::new();
            accesspasses.insert(accesspass1_pubkey, accesspass1.clone());
            accesspasses.insert(accesspass2_pubkey, accesspass2.clone());
            Ok(accesspasses)
        });

        client
            .expect_list_tenant()
            .returning(|_| Ok(std::collections::HashMap::new()));

        /*****************************************************************************************************/

        let mut output = Vec::new();
        let res = ListUserCliCommand {
            prepaid: false,
            solana_validator: false,
            solana_identity: None,
            device: None,
            location: None,
            owner: None,
            client_ip: None,
            user_type: None,
            cyoa_type: None,
            dz_ip: None,
            tunnel_id: None,
            status: None,
            multicast_group: None,
            tenant: None,
            all_tenants: false,
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | tenant                           | user_type | groups   | device       | location       | cyoa_type  | client_ip | dz_ip   | accesspass                  | tunnel_id | tunnel_net | status    | owner                                     \n 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo | 11111111111111111111111111111111 | Multicast | S:m_code | device1_code | location1_name | GREOverDIA | 1.2.3.4   | 2.3.4.5 | Prepaid: (expires epoch 10) | 500       | 1.2.3.5/32 | activated | 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo \n");

        let mut output = Vec::new();
        let res = ListUserCliCommand {
            prepaid: false,
            solana_validator: false,
            solana_identity: None,
            device: None,
            location: None,
            owner: None,
            client_ip: None,
            user_type: None,
            cyoa_type: None,
            dz_ip: None,
            tunnel_id: None,
            status: None,
            multicast_group: None,
            tenant: None,
            all_tenants: false,
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\",\"tenant\":\"11111111111111111111111111111111\",\"user_type\":\"Multicast\",\"device_pk\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9\",\"multicast\":\"S:m_code\",\"publishers\":\"\",\"subscribers\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo8\",\"device_name\":\"device1_code\",\"location_code\":\"location1_code\",\"location_name\":\"location1_name\",\"cyoa_type\":\"GREOverDIA\",\"client_ip\":\"1.2.3.4\",\"dz_ip\":\"2.3.4.5\",\"accesspass\":\"Prepaid: (expires epoch 10)\",\"tunnel_id\":500,\"tunnel_net\":\"1.2.3.5/32\",\"status\":\"Activated\",\"owner\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\"}]\n");
    }

    #[test]
    fn test_cli_user_list_filter_by_user_type() {
        let mut client = create_test_client();

        let user1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let user2_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUx");

        let device1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9");

        let user1 = User {
            account_type: AccountType::User,
            index: 1,
            bump_seed: 2,
            owner: user1_pubkey,
            user_type: UserType::IBRL,
            tenant_pk: Pubkey::default(),
            device_pk: device1_pubkey,
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 4].into(),
            dz_ip: [2, 3, 4, 5].into(),
            tunnel_id: 500,
            tunnel_net: "1.2.3.5/32".parse().unwrap(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        let user2 = User {
            account_type: AccountType::User,
            index: 2,
            bump_seed: 3,
            owner: user2_pubkey,
            user_type: UserType::Multicast,
            tenant_pk: Pubkey::default(),
            device_pk: device1_pubkey,
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 5].into(),
            dz_ip: [2, 3, 4, 6].into(),
            tunnel_id: 501,
            tunnel_net: "1.2.3.6/32".parse().unwrap(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        client.expect_list_user().returning(move |_| {
            let mut users = std::collections::HashMap::new();
            users.insert(user1_pubkey, user1.clone());
            users.insert(user2_pubkey, user2.clone());
            Ok(users)
        });

        client
            .expect_list_device()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_location()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_multicastgroup()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_accesspass()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_tenant()
            .returning(|_| Ok(std::collections::HashMap::new()));

        let mut output = Vec::new();
        let res = ListUserCliCommand {
            prepaid: false,
            solana_validator: false,
            solana_identity: None,
            device: None,
            location: None,
            owner: None,
            client_ip: None,
            user_type: Some(vec!["Multicast".to_string()]),
            cyoa_type: None,
            dz_ip: None,
            tunnel_id: None,
            status: None,
            multicast_group: None,
            tenant: None,
            all_tenants: false,
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);

        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("Multicast"));
        assert!(!output_str.contains("IBRL") || output_str.contains("Multicast"));
    }

    #[test]
    fn test_cli_user_list_filter_by_cyoa_type() {
        let mut client = create_test_client();

        let user1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let user2_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUx");

        let device1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9");

        let user1 = User {
            account_type: AccountType::User,
            index: 1,
            bump_seed: 2,
            owner: user1_pubkey,
            user_type: IBRL,
            tenant_pk: Pubkey::default(),
            device_pk: device1_pubkey,
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 4].into(),
            dz_ip: [2, 3, 4, 5].into(),
            tunnel_id: 500,
            tunnel_net: "1.2.3.5/32".parse().unwrap(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        let user2 = User {
            account_type: AccountType::User,
            index: 2,
            bump_seed: 3,
            owner: user2_pubkey,
            user_type: UserType::Multicast,
            tenant_pk: Pubkey::default(),
            device_pk: device1_pubkey,
            cyoa_type: UserCYOA::GREOverFabric,
            client_ip: [1, 2, 3, 5].into(),
            dz_ip: [2, 3, 4, 6].into(),
            tunnel_id: 501,
            tunnel_net: "1.2.3.6/32".parse().unwrap(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        client.expect_list_user().returning(move |_| {
            let mut users = std::collections::HashMap::new();
            users.insert(user1_pubkey, user1.clone());
            users.insert(user2_pubkey, user2.clone());
            Ok(users)
        });

        client
            .expect_list_device()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_location()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_multicastgroup()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_accesspass()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_tenant()
            .returning(|_| Ok(std::collections::HashMap::new()));

        let mut output = Vec::new();
        let res = ListUserCliCommand {
            prepaid: false,
            solana_validator: false,
            solana_identity: None,
            device: None,
            location: None,
            owner: None,
            client_ip: None,
            user_type: None,
            cyoa_type: Some(vec!["GREOverDIA".to_string()]),
            dz_ip: None,
            tunnel_id: None,
            status: None,
            multicast_group: None,
            tenant: None,
            all_tenants: false,
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);

        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("GREOverDIA"));
        assert!(!output_str.contains("GREOverFabric"));
    }

    #[test]
    fn test_cli_user_list_filter_by_dz_ip() {
        let mut client = create_test_client();

        let user1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let user2_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUx");

        let device1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9");

        let user1 = User {
            account_type: AccountType::User,
            index: 1,
            bump_seed: 2,
            owner: user1_pubkey,
            user_type: IBRL,
            tenant_pk: Pubkey::default(),
            device_pk: device1_pubkey,
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 4].into(),
            dz_ip: [2, 3, 4, 5].into(),
            tunnel_id: 500,
            tunnel_net: "1.2.3.5/32".parse().unwrap(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        let user2 = User {
            account_type: AccountType::User,
            index: 2,
            bump_seed: 3,
            owner: user2_pubkey,
            user_type: UserType::Multicast,
            tenant_pk: Pubkey::default(),
            device_pk: device1_pubkey,
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 5].into(),
            dz_ip: [2, 3, 4, 6].into(),
            tunnel_id: 501,
            tunnel_net: "1.2.3.6/32".parse().unwrap(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        client.expect_list_user().returning(move |_| {
            let mut users = std::collections::HashMap::new();
            users.insert(user1_pubkey, user1.clone());
            users.insert(user2_pubkey, user2.clone());
            Ok(users)
        });

        client
            .expect_list_device()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_location()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_multicastgroup()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_accesspass()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_tenant()
            .returning(|_| Ok(std::collections::HashMap::new()));

        let mut output = Vec::new();
        let res = ListUserCliCommand {
            prepaid: false,
            solana_validator: false,
            solana_identity: None,
            device: None,
            location: None,
            owner: None,
            client_ip: None,
            user_type: None,
            cyoa_type: None,
            dz_ip: Some(vec![[2, 3, 4, 5].into()]),
            tunnel_id: None,
            status: None,
            multicast_group: None,
            tenant: None,
            all_tenants: false,
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);

        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("2.3.4.5"));
        assert!(!output_str.contains("2.3.4.6"));
    }

    #[test]
    fn test_cli_user_list_filter_by_tunnel_id() {
        let mut client = create_test_client();

        let user1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let user2_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUx");

        let device1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9");

        let user1 = User {
            account_type: AccountType::User,
            index: 1,
            bump_seed: 2,
            owner: user1_pubkey,
            user_type: IBRL,
            tenant_pk: Pubkey::default(),
            device_pk: device1_pubkey,
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 4].into(),
            dz_ip: [2, 3, 4, 5].into(),
            tunnel_id: 500,
            tunnel_net: "1.2.3.5/32".parse().unwrap(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        let user2 = User {
            account_type: AccountType::User,
            index: 2,
            bump_seed: 3,
            owner: user2_pubkey,
            user_type: UserType::Multicast,
            tenant_pk: Pubkey::default(),
            device_pk: device1_pubkey,
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 5].into(),
            dz_ip: [2, 3, 4, 6].into(),
            tunnel_id: 501,
            tunnel_net: "1.2.3.6/32".parse().unwrap(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        client.expect_list_user().returning(move |_| {
            let mut users = std::collections::HashMap::new();
            users.insert(user1_pubkey, user1.clone());
            users.insert(user2_pubkey, user2.clone());
            Ok(users)
        });

        client
            .expect_list_device()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_location()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_multicastgroup()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_accesspass()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_tenant()
            .returning(|_| Ok(std::collections::HashMap::new()));

        let mut output = Vec::new();
        let res = ListUserCliCommand {
            prepaid: false,
            solana_validator: false,
            solana_identity: None,
            device: None,
            location: None,
            owner: None,
            client_ip: None,
            user_type: None,
            cyoa_type: None,
            dz_ip: None,
            tunnel_id: Some(vec![500]),
            status: None,
            multicast_group: None,
            tenant: None,
            all_tenants: false,
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);

        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("500"));
        assert!(!output_str.contains("501") || output_str.contains("500"));
    }

    #[test]
    fn test_cli_user_list_filter_by_status() {
        let mut client = create_test_client();

        let user1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let user2_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUx");

        let device1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9");

        let user1 = User {
            account_type: AccountType::User,
            index: 1,
            bump_seed: 2,
            owner: user1_pubkey,
            user_type: IBRL,
            tenant_pk: Pubkey::default(),
            device_pk: device1_pubkey,
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 4].into(),
            dz_ip: [2, 3, 4, 5].into(),
            tunnel_id: 500,
            tunnel_net: "1.2.3.5/32".parse().unwrap(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        let user2 = User {
            account_type: AccountType::User,
            index: 2,
            bump_seed: 3,
            owner: user2_pubkey,
            user_type: UserType::Multicast,
            tenant_pk: Pubkey::default(),
            device_pk: device1_pubkey,
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 5].into(),
            dz_ip: [2, 3, 4, 6].into(),
            tunnel_id: 501,
            tunnel_net: "1.2.3.6/32".parse().unwrap(),
            status: UserStatus::Pending,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        client.expect_list_user().returning(move |_| {
            let mut users = std::collections::HashMap::new();
            users.insert(user1_pubkey, user1.clone());
            users.insert(user2_pubkey, user2.clone());
            Ok(users)
        });

        client
            .expect_list_device()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_location()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_multicastgroup()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_accesspass()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_tenant()
            .returning(|_| Ok(std::collections::HashMap::new()));

        let mut output = Vec::new();
        let res = ListUserCliCommand {
            prepaid: false,
            solana_validator: false,
            solana_identity: None,
            device: None,
            location: None,
            owner: None,
            client_ip: None,
            user_type: None,
            cyoa_type: None,
            dz_ip: None,
            tunnel_id: None,
            status: Some(vec!["activated".to_string()]),
            multicast_group: None,
            tenant: None,
            all_tenants: false,
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);

        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("activated"));
        assert!(!output_str.contains("pending"));
    }

    #[test]
    fn test_cli_user_list_filter_by_multicast_group() {
        let mut client = create_test_client();

        let user1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let user2_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUx");

        let device1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9");
        let mgroup1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo8");

        let mgroup1 = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 2,
            tenant_pk: Pubkey::default(),
            code: "m_code".to_string(),
            multicast_ip: [1, 2, 3, 4].into(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
            publisher_count: 0,
            subscriber_count: 0,
        };

        let user1 = User {
            account_type: AccountType::User,
            index: 1,
            bump_seed: 2,
            owner: user1_pubkey,
            user_type: IBRL,
            tenant_pk: Pubkey::default(),
            device_pk: device1_pubkey,
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 4].into(),
            dz_ip: [2, 3, 4, 5].into(),
            tunnel_id: 500,
            tunnel_net: "1.2.3.5/32".parse().unwrap(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        let user2 = User {
            account_type: AccountType::User,
            index: 2,
            bump_seed: 3,
            owner: user2_pubkey,
            user_type: UserType::Multicast,
            tenant_pk: Pubkey::default(),
            device_pk: device1_pubkey,
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 5].into(),
            dz_ip: [2, 3, 4, 6].into(),
            tunnel_id: 501,
            tunnel_net: "1.2.3.6/32".parse().unwrap(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![mgroup1_pubkey],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        client.expect_list_user().returning(move |_| {
            let mut users = std::collections::HashMap::new();
            users.insert(user1_pubkey, user1.clone());
            users.insert(user2_pubkey, user2.clone());
            Ok(users)
        });

        client
            .expect_list_device()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_location()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client.expect_list_multicastgroup().returning(move |_| {
            let mut mgroups = std::collections::HashMap::new();
            mgroups.insert(mgroup1_pubkey, mgroup1.clone());
            Ok(mgroups)
        });
        client
            .expect_list_accesspass()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_tenant()
            .returning(|_| Ok(std::collections::HashMap::new()));

        let mut output = Vec::new();
        let res = ListUserCliCommand {
            prepaid: false,
            solana_validator: false,
            solana_identity: None,
            device: None,
            location: None,
            owner: None,
            client_ip: None,
            user_type: None,
            cyoa_type: None,
            dz_ip: None,
            tunnel_id: None,
            status: None,
            multicast_group: Some(vec!["m_code".to_string()]),
            tenant: None,
            all_tenants: false,
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);

        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("m_code"));
        assert!(output_str.contains("Multicast"));
        assert!(!output_str.contains("IBRL"));
    }

    #[test]
    fn test_cli_user_list_filter_by_tenant_code() {
        let mut client = create_test_client();

        let tenant1_pubkey = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let tenant2_pubkey = Pubkey::from_str_const("DDddB7bhR9azxLAUEH7ZVtW168wRdreiDKhi4McDfKZt");

        let tenant1 = Tenant {
            account_type: AccountType::Tenant,
            owner: Pubkey::default(),
            bump_seed: 1,
            code: "tenant1".to_string(),
            vrf_id: 1,
            reference_count: 0,
            administrators: vec![],
            payment_status: TenantPaymentStatus::Paid,
            token_account: Pubkey::default(),
        };

        let tenant2 = Tenant {
            account_type: AccountType::Tenant,
            owner: Pubkey::default(),
            bump_seed: 2,
            code: "tenant2".to_string(),
            vrf_id: 2,
            reference_count: 0,
            administrators: vec![],
            payment_status: TenantPaymentStatus::Paid,
            token_account: Pubkey::default(),
        };

        let user1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let user2_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUx");

        let device1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9");

        let user1 = User {
            account_type: AccountType::User,
            index: 1,
            bump_seed: 2,
            owner: user1_pubkey,
            user_type: IBRL,
            tenant_pk: tenant1_pubkey,
            device_pk: device1_pubkey,
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 4].into(),
            dz_ip: [2, 3, 4, 5].into(),
            tunnel_id: 500,
            tunnel_net: "1.2.3.5/32".parse().unwrap(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        let user2 = User {
            account_type: AccountType::User,
            index: 2,
            bump_seed: 3,
            owner: user2_pubkey,
            user_type: UserType::Multicast,
            tenant_pk: tenant2_pubkey,
            device_pk: device1_pubkey,
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 5].into(),
            dz_ip: [2, 3, 4, 6].into(),
            tunnel_id: 501,
            tunnel_net: "1.2.3.6/32".parse().unwrap(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        client.expect_list_user().returning(move |_| {
            let mut users = std::collections::HashMap::new();
            users.insert(user1_pubkey, user1.clone());
            users.insert(user2_pubkey, user2.clone());
            Ok(users)
        });

        client
            .expect_list_device()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_location()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_multicastgroup()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_accesspass()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client.expect_list_tenant().returning(move |_| {
            let mut tenants = std::collections::HashMap::new();
            tenants.insert(tenant1_pubkey, tenant1.clone());
            tenants.insert(tenant2_pubkey, tenant2.clone());
            Ok(tenants)
        });

        // Filter by tenant code
        let mut output = Vec::new();
        let res = ListUserCliCommand {
            prepaid: false,
            solana_validator: false,
            solana_identity: None,
            device: None,
            location: None,
            owner: None,
            client_ip: None,
            user_type: None,
            cyoa_type: None,
            dz_ip: None,
            tunnel_id: None,
            status: None,
            multicast_group: None,
            tenant: Some(vec!["tenant1".to_string()]),
            all_tenants: false,
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);

        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("tenant1"));
        assert!(!output_str.contains("tenant2"));

        // Filter by tenant pubkey
        let mut output = Vec::new();
        let res = ListUserCliCommand {
            prepaid: false,
            solana_validator: false,
            solana_identity: None,
            device: None,
            location: None,
            owner: None,
            client_ip: None,
            user_type: None,
            cyoa_type: None,
            dz_ip: None,
            tunnel_id: None,
            status: None,
            multicast_group: None,
            tenant: Some(vec![tenant2_pubkey.to_string()]),
            all_tenants: false,
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);

        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("tenant2"));
        assert!(!output_str.contains("tenant1"));
    }

    #[test]
    fn test_cli_user_list_all_tenants() {
        let mut client = create_test_client();

        let tenant1_pubkey = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let tenant2_pubkey = Pubkey::from_str_const("DDddB7bhR9azxLAUEH7ZVtW168wRdreiDKhi4McDfKZt");

        let tenant1 = Tenant {
            account_type: AccountType::Tenant,
            owner: Pubkey::default(),
            bump_seed: 1,
            code: "tenant1".to_string(),
            vrf_id: 1,
            reference_count: 0,
            administrators: vec![],
            payment_status: TenantPaymentStatus::Paid,
            token_account: Pubkey::default(),
        };

        let tenant2 = Tenant {
            account_type: AccountType::Tenant,
            owner: Pubkey::default(),
            bump_seed: 2,
            code: "tenant2".to_string(),
            vrf_id: 2,
            reference_count: 0,
            administrators: vec![],
            payment_status: TenantPaymentStatus::Paid,
            token_account: Pubkey::default(),
        };

        let user1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let user2_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUx");

        let device1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9");

        let user1 = User {
            account_type: AccountType::User,
            index: 1,
            bump_seed: 2,
            owner: user1_pubkey,
            user_type: IBRL,
            tenant_pk: tenant1_pubkey,
            device_pk: device1_pubkey,
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 4].into(),
            dz_ip: [2, 3, 4, 5].into(),
            tunnel_id: 500,
            tunnel_net: "1.2.3.5/32".parse().unwrap(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        let user2 = User {
            account_type: AccountType::User,
            index: 2,
            bump_seed: 3,
            owner: user2_pubkey,
            user_type: UserType::Multicast,
            tenant_pk: tenant2_pubkey,
            device_pk: device1_pubkey,
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 5].into(),
            dz_ip: [2, 3, 4, 6].into(),
            tunnel_id: 501,
            tunnel_net: "1.2.3.6/32".parse().unwrap(),
            status: Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
        };

        client.expect_list_user().returning(move |_| {
            let mut users = std::collections::HashMap::new();
            users.insert(user1_pubkey, user1.clone());
            users.insert(user2_pubkey, user2.clone());
            Ok(users)
        });

        client
            .expect_list_device()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_location()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_multicastgroup()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client
            .expect_list_accesspass()
            .returning(|_| Ok(std::collections::HashMap::new()));
        client.expect_list_tenant().returning(move |_| {
            let mut tenants = std::collections::HashMap::new();
            tenants.insert(tenant1_pubkey, tenant1.clone());
            tenants.insert(tenant2_pubkey, tenant2.clone());
            Ok(tenants)
        });

        // --no-tenant should show users from all tenants
        let mut output = Vec::new();
        let res = ListUserCliCommand {
            prepaid: false,
            solana_validator: false,
            solana_identity: None,
            device: None,
            location: None,
            owner: None,
            client_ip: None,
            user_type: None,
            cyoa_type: None,
            dz_ip: None,
            tunnel_id: None,
            status: None,
            multicast_group: None,
            tenant: None,
            all_tenants: true,
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);

        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("tenant1"));
        assert!(output_str.contains("tenant2"));
    }
}
