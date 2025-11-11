use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_program_common::{
    serializer,
    types::{parse_utils::bandwidth_to_string, NetworkV4},
};
use doublezero_sdk::{
    commands::{
        contributor::{get::GetContributorCommand, list::ListContributorCommand},
        device::list::ListDeviceCommand,
        link::list::ListLinkCommand,
    },
    Link, LinkLinkType, LinkStatus,
};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct ListLinkCliCommand {
    /// Filter by contributor (pubkey or code)
    #[arg(long, short = 'c')]
    pub contributor: Option<String>,
    /// List only WAN links.
    #[arg(long, default_value_t = false)]
    pub wan: bool,
    /// List only DXZ links.
    #[arg(long, default_value_t = false)]
    pub dzx: bool,
    /// Output as pretty JSON.
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON.
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct LinkDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    #[tabled(rename = "contributor")]
    pub contributor_code: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(rename = "side_a")]
    #[tabled(skip)]
    pub side_a_pk: Pubkey,
    pub side_a_name: String,
    pub side_a_iface_name: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(rename = "side_z")]
    #[tabled(skip)]
    pub side_z_pk: Pubkey,
    pub side_z_name: String,
    pub side_z_iface_name: String,
    pub link_type: LinkLinkType,
    pub bandwidth: String,
    pub mtu: u32,
    #[tabled(display = "crate::util::display_as_ms", rename = "delay_ms")]
    pub delay_ns: u64,
    #[tabled(display = "crate::util::display_as_ms", rename = "jitter_ms")]
    pub jitter_ns: u64,
    #[tabled(display = "crate::util::display_as_ms", rename = "delay_override_ms")]
    pub delay_override_ns: u64,
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
    pub status: LinkStatus,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListLinkCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let contributors = client.list_contributor(ListContributorCommand {})?;
        let devices = client.list_device(ListDeviceCommand)?;
        let mut links = client.list_link(ListLinkCommand)?;

        // Filter by contributor if specified
        if let Some(contributor_filter) = &self.contributor {
            let contributor_pk = match client.get_contributor(GetContributorCommand {
                pubkey_or_code: contributor_filter.clone(),
            }) {
                Ok((pk, _)) => pk,
                Err(_) => {
                    return Err(eyre::eyre!(
                        "Contributor '{}' not found",
                        contributor_filter
                    ));
                }
            };
            links.retain(|_, link| link.contributor_pk == contributor_pk);
        }

        let mut links: Vec<(Pubkey, Link)> = links.into_iter().collect();
        if self.wan {
            links.retain(|(_, link)| link.link_type == LinkLinkType::WAN);
        }
        if self.dzx {
            links.retain(|(_, link)| link.link_type == LinkLinkType::DZX);
        }

        let mut tunnel_displays: Vec<LinkDisplay> = links
            .into_iter()
            .map(|(pubkey, link)| {
                let contributor_code = match contributors.get(&link.contributor_pk) {
                    Some(contributor) => contributor.code.clone(),
                    None => link.contributor_pk.to_string(),
                };
                let side_a_name = match devices.get(&link.side_a_pk) {
                    Some(device) => device.code.clone(),
                    None => link.side_a_pk.to_string(),
                };
                let side_z_name = match devices.get(&link.side_z_pk) {
                    Some(device) => device.code.clone(),
                    None => link.side_z_pk.to_string(),
                };

                LinkDisplay {
                    account: pubkey,
                    code: link.code,
                    contributor_code,
                    side_a_pk: link.side_a_pk,
                    side_a_name,
                    side_a_iface_name: link.side_a_iface_name,
                    side_z_pk: link.side_z_pk,
                    side_z_name,
                    side_z_iface_name: link.side_z_iface_name,
                    link_type: link.link_type,
                    bandwidth: bandwidth_to_string(&link.bandwidth),
                    mtu: link.mtu,
                    delay_ns: link.delay_ns,
                    jitter_ns: link.jitter_ns,
                    delay_override_ns: link.delay_override_ns,
                    tunnel_id: link.tunnel_id,
                    tunnel_net: link.tunnel_net,
                    status: link.status,
                    owner: link.owner,
                }
            })
            .collect();

        tunnel_displays.sort_by(|a, b| {
            a.side_a_name
                .cmp(&b.side_a_name)
                .then(a.side_z_name.cmp(&b.side_z_name))
                .then(a.code.cmp(&b.code))
        });

        let res = if self.json {
            serde_json::to_string_pretty(&tunnel_displays)?
        } else if self.json_compact {
            serde_json::to_string(&tunnel_displays)?
        } else {
            Table::new(tunnel_displays)
                .with(Style::psql().remove_horizontals())
                .to_string()
        };

        writeln!(out, "{res}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{link::list::ListLinkCliCommand, tests::utils::create_test_client};

    use doublezero_sdk::{
        commands::contributor::get::GetContributorCommand, Contributor, ContributorStatus, Device,
        DeviceStatus, DeviceType, Link, LinkLinkType, LinkStatus,
    };
    use doublezero_serviceability::state::accounttype::AccountType;
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    #[test]
    fn test_cli_link_list() {
        let mut client = create_test_client();

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let contributor = Contributor {
            account_type: AccountType::Contributor,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "contributor1_code".to_string(),
            status: ContributorStatus::Activated,
            owner: contributor_pk,
        };

        client.expect_list_contributor().returning(move |_| {
            let mut contributors = HashMap::new();
            contributors.insert(contributor_pk, contributor.clone());
            Ok(contributors)
        });

        let location1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1");
        let location2_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo2");
        let exchange1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo3");
        let exchange2_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo4");

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
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
        };
        let device2_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9");
        let device2 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "device2_code".to_string(),
            contributor_pk,
            location_pk: location2_pubkey,
            exchange_pk: exchange2_pubkey,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
        };

        client.expect_list_device().returning(move |_| {
            let mut devices = HashMap::new();
            devices.insert(device1_pubkey, device1.clone());
            devices.insert(device2_pubkey, device2.clone());
            Ok(devices)
        });

        let tunnel1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR");
        let tunnel1 = Link {
            account_type: AccountType::Link,
            index: 1,
            bump_seed: 2,
            code: "tunnel_code".to_string(),
            contributor_pk,
            side_a_pk: device1_pubkey,
            side_z_pk: device2_pubkey,
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 4500,
            delay_ns: 20_000,
            jitter_ns: 1121,
            delay_override_ns: 0,
            tunnel_id: 1234,
            tunnel_net: "1.2.3.4/32".parse().unwrap(),
            status: LinkStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
        };

        client.expect_list_link().returning(move |_| {
            let mut tunnels = HashMap::new();
            tunnels.insert(tunnel1_pubkey, tunnel1.clone());
            Ok(tunnels)
        });

        let mut output = Vec::new();
        let res = ListLinkCliCommand {
            contributor: None,
            wan: false,
            dzx: false,
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | code        | contributor       | side_a_name  | side_a_iface_name | side_z_name  | side_z_iface_name | link_type | bandwidth | mtu  | delay_ms | jitter_ms | delay_override_ms | tunnel_id | tunnel_net | status    | owner                                     \n 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR | tunnel_code | contributor1_code | device2_code | eth0              | device2_code | eth1              | WAN       | 10Gbps    | 4500 | 0.02ms   | 0.00ms    | 0.00ms            | 1234      | 1.2.3.4/32 | activated | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9 \n");

        let mut output = Vec::new();
        let res = ListLinkCliCommand {
            contributor: None,
            wan: false,
            dzx: false,
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR\",\"code\":\"tunnel_code\",\"contributor_code\":\"contributor1_code\",\"side_a_pk\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9\",\"side_a_name\":\"device2_code\",\"side_a_iface_name\":\"eth0\",\"side_z_pk\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9\",\"side_z_name\":\"device2_code\",\"side_z_iface_name\":\"eth1\",\"link_type\":\"WAN\",\"bandwidth\":\"10Gbps\",\"mtu\":4500,\"delay_ns\":20000,\"jitter_ns\":1121,\"delay_override_ns\":0,\"tunnel_id\":1234,\"tunnel_net\":\"1.2.3.4/32\",\"status\":\"Activated\",\"owner\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9\"}]\n");
    }

    #[test]
    fn test_cli_link_list_filtered_by_contributor() {
        let mut client = create_test_client();

        let contributor1_pk =
            Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let contributor1 = Contributor {
            account_type: AccountType::Contributor,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "contributor1_code".to_string(),
            status: ContributorStatus::Activated,
            owner: contributor1_pk,
        };
        let contributor2_pk = Pubkey::new_unique();
        let contributor2 = Contributor {
            account_type: AccountType::Contributor,
            index: 2,
            bump_seed: 3,
            reference_count: 0,
            code: "contributor2_code".to_string(),
            status: ContributorStatus::Activated,
            owner: contributor2_pk,
        };

        let contributor1_for_list = contributor1.clone();
        let contributor2_for_list = contributor2.clone();
        client.expect_list_contributor().returning(move |_| {
            let mut contributors = HashMap::new();
            contributors.insert(contributor1_pk, contributor1_for_list.clone());
            contributors.insert(contributor2_pk, contributor2_for_list.clone());
            Ok(contributors)
        });

        let contributor_lookup = contributor1.clone();
        client
            .expect_get_contributor()
            .with(predicate::eq(GetContributorCommand {
                pubkey_or_code: "contributor1_code".to_string(),
            }))
            .returning(move |_| Ok((contributor1_pk, contributor_lookup.clone())));

        let location1_pubkey = Pubkey::new_unique();
        let location2_pubkey = Pubkey::new_unique();
        let exchange1_pubkey = Pubkey::new_unique();
        let exchange2_pubkey = Pubkey::new_unique();

        let device1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            reference_count: 0,
            code: "device1_code".to_string(),
            contributor_pk: contributor1_pk,
            location_pk: location1_pubkey,
            exchange_pk: exchange1_pubkey,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
            metrics_publisher_pk: Pubkey::default(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
        };
        let device2_pubkey = Pubkey::new_unique();
        let device2 = Device {
            account_type: AccountType::Device,
            index: 2,
            bump_seed: 3,
            reference_count: 0,
            code: "device2_code".to_string(),
            contributor_pk: contributor2_pk,
            location_pk: location2_pubkey,
            exchange_pk: exchange2_pubkey,
            device_type: DeviceType::Switch,
            public_ip: [5, 6, 7, 8].into(),
            dz_prefixes: "5.6.7.8/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            owner: Pubkey::new_unique(),
            metrics_publisher_pk: Pubkey::new_unique(),
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
        };

        client.expect_list_device().returning(move |_| {
            let mut devices = HashMap::new();
            devices.insert(device1_pubkey, device1.clone());
            devices.insert(device2_pubkey, device2.clone());
            Ok(devices)
        });

        let tunnel1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR");
        let tunnel1 = Link {
            account_type: AccountType::Link,
            index: 1,
            bump_seed: 2,
            code: "tunnel_code".to_string(),
            contributor_pk: contributor1_pk,
            side_a_pk: device1_pubkey,
            side_z_pk: device2_pubkey,
            link_type: LinkLinkType::WAN,
            bandwidth: 10_000_000_000,
            mtu: 4500,
            delay_ns: 20_000,
            jitter_ns: 1121,
            delay_override_ns: 0,
            tunnel_id: 1234,
            tunnel_net: "1.2.3.4/32".parse().unwrap(),
            status: LinkStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
        };
        let tunnel2_pubkey = Pubkey::new_unique();
        let tunnel2 = Link {
            account_type: AccountType::Link,
            index: 2,
            bump_seed: 3,
            code: "tunnel_code_two".to_string(),
            contributor_pk: contributor2_pk,
            side_a_pk: device2_pubkey,
            side_z_pk: device1_pubkey,
            link_type: LinkLinkType::WAN,
            bandwidth: 5_000_000_000,
            mtu: 1500,
            delay_ns: 40_000,
            jitter_ns: 2000,
            delay_override_ns: 0,
            tunnel_id: 5678,
            tunnel_net: "5.6.7.8/32".parse().unwrap(),
            status: LinkStatus::Activated,
            owner: Pubkey::new_unique(),
            side_a_iface_name: "eth2".to_string(),
            side_z_iface_name: "eth3".to_string(),
        };

        client.expect_list_link().returning(move |_| {
            let mut tunnels = HashMap::new();
            tunnels.insert(tunnel1_pubkey, tunnel1.clone());
            tunnels.insert(tunnel2_pubkey, tunnel2.clone());
            Ok(tunnels)
        });

        let mut output = Vec::new();
        let res = ListLinkCliCommand {
            contributor: Some("contributor1_code".to_string()),
            wan: false,
            dzx: false,
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("tunnel_code"));
        assert!(!output_str.contains("tunnel_code_two"));
    }
}
