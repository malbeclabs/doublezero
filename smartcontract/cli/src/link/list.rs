use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::{
    commands::{device::list::ListDeviceCommand, link::list::ListLinkCommand},
    *,
};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct ListLinkCliCommand {
    /// Output as pretty JSON.
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON.
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct LinkDisplay {
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    #[tabled(rename = "side_a")]
    pub side_a_pk: Pubkey,
    #[tabled(skip)]
    pub side_a_name: String,
    pub side_a_iface_name: String,
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    #[tabled(rename = "side_z")]
    pub side_z_pk: Pubkey,
    #[tabled(skip)]
    pub side_z_name: String,
    pub side_z_iface_name: String,
    pub link_type: LinkLinkType,
    pub bandwidth: u64,
    pub mtu: u32,
    #[tabled(display = "crate::util::display_as_ms", rename = "delay_ms")]
    pub delay_ns: u64,
    #[tabled(display = "crate::util::display_as_ms", rename = "jitter_ms")]
    pub jitter_ns: u64,
    pub tunnel_id: u16,
    pub tunnel_net: NetworkV4,
    pub status: LinkStatus,
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListLinkCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let devices = client.list_device(ListDeviceCommand)?;
        let tunnels = client.list_link(ListLinkCommand)?;

        let mut tunnels: Vec<(Pubkey, Link)> = tunnels.into_iter().collect();
        tunnels.sort_by(|(_, a), (_, b)| a.owner.cmp(&b.owner).then(a.tunnel_id.cmp(&b.tunnel_id)));

        let tunnel_displays: Vec<LinkDisplay> = tunnels
            .into_iter()
            .map(|(pubkey, tunnel)| {
                let side_a_name = match devices.get(&tunnel.side_a_pk) {
                    Some(device) => device.code.clone(),
                    None => tunnel.side_a_pk.to_string(),
                };
                let side_z_name = match devices.get(&tunnel.side_z_pk) {
                    Some(device) => device.code.clone(),
                    None => tunnel.side_z_pk.to_string(),
                };

                LinkDisplay {
                    account: pubkey,
                    code: tunnel.code,
                    side_a_pk: tunnel.side_a_pk,
                    side_a_name,
                    side_a_iface_name: tunnel.side_a_iface_name,
                    side_z_pk: tunnel.side_z_pk,
                    side_z_name,
                    side_z_iface_name: tunnel.side_z_iface_name,
                    link_type: tunnel.link_type,
                    bandwidth: tunnel.bandwidth,
                    mtu: tunnel.mtu,
                    delay_ns: tunnel.delay_ns,
                    jitter_ns: tunnel.jitter_ns,
                    tunnel_id: tunnel.tunnel_id,
                    tunnel_net: tunnel.tunnel_net,
                    status: tunnel.status,
                    owner: tunnel.owner,
                }
            })
            .collect();

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

    use doublezero_sdk::{Device, DeviceStatus, DeviceType, Link, LinkLinkType, LinkStatus};
    use doublezero_serviceability::state::accounttype::AccountType;
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    #[test]
    fn test_cli_link_list() {
        let mut client = create_test_client();

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let location1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1");
        let location2_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo2");
        let exchange1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo3");
        let exchange2_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo4");

        let device1_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9");
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
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
            bgp_asn: 0,
            dia_bgp_asn: 0,
            mgmt_vrf: "default".to_string(),
            dns_servers: vec![[8, 8, 8, 8].into(), [8, 8, 4, 4].into()],
            ntp_servers: vec![[192, 168, 1, 1].into(), [192, 168, 1, 2].into()],
            interfaces: vec![],
        };
        let device2_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9");
        let device2 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
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
            bgp_asn: 0,
            dia_bgp_asn: 0,
            mgmt_vrf: "default".to_string(),
            dns_servers: vec![[8, 8, 8, 8].into(), [8, 8, 4, 4].into()],
            ntp_servers: vec![[192, 168, 1, 1].into(), [192, 168, 1, 2].into()],
            interfaces: vec![],
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
            link_type: LinkLinkType::L3,
            bandwidth: 1234,
            mtu: 1566,
            delay_ns: 1234,
            jitter_ns: 1121,
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
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | code        | side_a                                    | side_a_iface_name | side_z                                    | side_z_iface_name | link_type | bandwidth | mtu  | delay_ms | jitter_ms | tunnel_id | tunnel_net | status    | owner                                     \n 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR | tunnel_code | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9 | eth0              | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9 | eth1              | L3        | 1234      | 1566 | 0.00ms   | 0.00ms    | 1234      | 1.2.3.4/32 | activated | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9 \n");

        let mut output = Vec::new();
        let res = ListLinkCliCommand {
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR\",\"code\":\"tunnel_code\",\"side_a_pk\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9\",\"side_a_name\":\"device2_code\",\"side_a_iface_name\":\"eth0\",\"side_z_pk\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9\",\"side_z_name\":\"device2_code\",\"side_z_iface_name\":\"eth1\",\"link_type\":\"L3\",\"bandwidth\":1234,\"mtu\":1566,\"delay_ns\":1234,\"jitter_ns\":1121,\"tunnel_id\":1234,\"tunnel_net\":\"1.2.3.4/32\",\"status\":\"Activated\",\"owner\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9\"}]\n");
    }
}
