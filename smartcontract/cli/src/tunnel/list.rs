use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::device::list::ListDeviceCommand;
use doublezero_sdk::commands::tunnel::list::ListTunnelCommand;
use doublezero_sdk::*;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

const NANOS_TO_MS: f32 = 1000000.0;

#[derive(Args, Debug)]
pub struct ListTunnelCliCommand {
    #[arg(long, default_value_t = false)]
    pub json: bool,
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct TunnelDisplay {
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    #[tabled(rename = "side_a")]
    pub side_a_pk: Pubkey,
    #[tabled(skip)]
    pub side_a_name: String,
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    #[tabled(rename = "side_z")]
    pub side_z_pk: Pubkey,
    #[tabled(skip)]
    pub side_z_name: String,
    pub tunnel_type: TunnelTunnelType,
    pub bandwidth: u64,
    pub mtu: u32,
    #[tabled(display = "display_as_ms", rename = "delay_ms")]
    pub delay_ns: u64,
    #[tabled(display = "display_as_ms", rename = "jitter_ms")]
    pub jitter_ns: u64,
    pub tunnel_id: u16,
    #[tabled(display = "doublezero_sla_program::types::networkv4_to_string")]
    #[serde(serialize_with = "crate::serializer::serialize_networkv4_as_string")]
    pub tunnel_net: NetworkV4,
    pub status: TunnelStatus,
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

fn display_as_ms(latency: &u64) -> String {
    format!("{:.2}ms", (*latency as f32 / NANOS_TO_MS))
}

impl ListTunnelCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let devices = client.list_device(ListDeviceCommand {})?;
        let tunnels = client.list_tunnel(ListTunnelCommand {})?;

        let mut tunnels: Vec<(Pubkey, Tunnel)> = tunnels.into_iter().collect();
        tunnels.sort_by(|(_, a), (_, b)| a.owner.cmp(&b.owner).then(a.tunnel_id.cmp(&b.tunnel_id)));

        let tunnel_displays: Vec<TunnelDisplay> = tunnels
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

                TunnelDisplay {
                    account: pubkey,
                    code: tunnel.code,
                    side_a_pk: tunnel.side_a_pk,
                    side_a_name,
                    side_z_pk: tunnel.side_z_pk,
                    side_z_name,
                    tunnel_type: tunnel.tunnel_type,
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

        writeln!(out, "{}", res)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::tests::tests::create_test_client;
    use crate::tunnel::list::ListTunnelCliCommand;
    use doublezero_sdk::{
        Device, DeviceStatus, DeviceType, Tunnel, TunnelStatus, TunnelTunnelType,
    };
    use doublezero_sla_program::state::accounttype::AccountType;
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    #[test]
    fn test_cli_tunnel_list() {
        let mut client = create_test_client();

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
            location_pk: location1_pubkey,
            exchange_pk: exchange1_pubkey,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4],
            dz_prefixes: vec![([1, 2, 3, 4], 32)],
            status: DeviceStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
        };
        let device2_pubkey = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9");
        let device2 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            code: "device2_code".to_string(),
            location_pk: location2_pubkey,
            exchange_pk: exchange2_pubkey,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4],
            dz_prefixes: vec![([1, 2, 3, 4], 32)],
            status: DeviceStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
        };

        client.expect_list_device().returning(move |_| {
            let mut devices = HashMap::new();
            devices.insert(device1_pubkey, device1.clone());
            devices.insert(device2_pubkey, device2.clone());
            Ok(devices)
        });

        let tunnel1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR");
        let tunnel1 = Tunnel {
            account_type: AccountType::Tunnel,
            index: 1,
            bump_seed: 2,
            code: "tunnel_code".to_string(),
            side_a_pk: device1_pubkey,
            side_z_pk: device2_pubkey,
            tunnel_type: TunnelTunnelType::MPLSoGRE,
            bandwidth: 1234,
            mtu: 1566,
            delay_ns: 1234,
            jitter_ns: 1121,
            tunnel_id: 1234,
            tunnel_net: ([1, 2, 3, 4], 32).into(),
            status: TunnelStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
        };

        client.expect_list_tunnel().returning(move |_| {
            let mut tunnels = HashMap::new();
            tunnels.insert(tunnel1_pubkey, tunnel1.clone());
            Ok(tunnels)
        });

        let mut output = Vec::new();
        let res = ListTunnelCliCommand {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | code        | side_a                                    | side_z                                    | tunnel_type | bandwidth | mtu  | delay_ms | jitter_ms | tunnel_id | tunnel_net | status    | owner                                     \n 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR | tunnel_code | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9 | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9 | MPLSoGRE    | 1234      | 1566 | 0.00ms   | 0.00ms    | 1234      | 1.2.3.4/32 | activated | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9 \n");

        let mut output = Vec::new();
        let res = ListTunnelCliCommand {
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR\",\"code\":\"tunnel_code\",\"side_a_pk\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9\",\"side_a_name\":\"device2_code\",\"side_z_pk\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9\",\"side_z_name\":\"device2_code\",\"tunnel_type\":\"MPLSoGRE\",\"bandwidth\":1234,\"mtu\":1566,\"delay_ns\":1234,\"jitter_ns\":1121,\"tunnel_id\":1234,\"tunnel_net\":\"1.2.3.4/32\",\"status\":\"Activated\",\"owner\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9\"}]\n");
    }
}
