use std::io::Write;

use clap::Args;
use doublezero_sdk::commands::device::list::ListDeviceCommand;
use doublezero_sdk::commands::tunnel::list::ListTunnelCommand;
use doublezero_sdk::*;
use prettytable::{format, row, Cell, Row, Table};

#[derive(Args, Debug)]
pub struct ListTunnelArgs {
    #[arg(long)]
    pub code: Option<String>,
}

impl ListTunnelArgs {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        let mut table = Table::new();
        table.add_row(row![
            "pubkey",
            "code",
            "side_a",
            "side_z",
            "tunnel_type",
            "bandwidth",
            "mtu",
            "delay_ms",
            "jitter_ms",
            "tunnel_id",
            "tunnel_net",
            "status",
            "owner"
        ]);

        let devices = ListDeviceCommand {}.execute(client)?;
        let tunnels = ListTunnelCommand {}.execute(client)?;

        for (pubkey, data) in tunnels {
            let side_a_name = match &devices.get(&data.side_a_pk) {
                Some(device) => &device.code,
                None => &data.side_a_pk.to_string(),
            };
            let side_z_name = match &devices.get(&data.side_z_pk) {
                Some(device) => &device.code,
                None => &data.side_z_pk.to_string(),
            };

            table.add_row(Row::new(vec![
                Cell::new(&pubkey.to_string()),
                Cell::new(&data.code),
                Cell::new(side_a_name),
                Cell::new(side_z_name),
                Cell::new(&data.tunnel_type.to_string()),
                Cell::new(&bandwidth_to_string(data.bandwidth)),
                Cell::new_align(&data.mtu.to_string(), format::Alignment::RIGHT),
                Cell::new_align(&delay_to_string(data.delay_ns), format::Alignment::RIGHT),
                Cell::new_align(&jitter_to_string(data.jitter_ns), format::Alignment::RIGHT),
                Cell::new(&data.tunnel_id.to_string()),
                Cell::new(&networkv4_to_string(&data.tunnel_net)),
                Cell::new(&data.status.to_string()),
                Cell::new(&data.owner.to_string()),
            ]));
        }

        table.set_format(*format::consts::FORMAT_NO_BORDER_LINE_SEPARATOR);
        table.print(out)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::tests::tests::create_test_client;
    use crate::tunnel::list::ListTunnelArgs;
    use doublezero_sdk::{
        Device, DeviceStatus, DeviceType, Tunnel, TunnelStatus, TunnelTunnelType,
    };
    use doublezero_sla_program::state::{accountdata::AccountData, accounttype::AccountType};
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_tunnel_list() {
        let mut client = create_test_client();

        let location1_pubkey = Pubkey::new_unique();
        let location2_pubkey = Pubkey::new_unique();
        let exchange1_pubkey = Pubkey::new_unique();
        let exchange2_pubkey = Pubkey::new_unique();

        let device1_pubkey = Pubkey::new_unique();
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
            owner: Pubkey::new_unique(),
        };
        let device2_pubkey = Pubkey::new_unique();
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
            owner: Pubkey::new_unique(),
        };

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Device))
            .returning(move |_| {
                let mut devices = HashMap::new();
                devices.insert(device1_pubkey, AccountData::Device(device1.clone()));
                devices.insert(device2_pubkey, AccountData::Device(device2.clone()));
                Ok(devices)
            });

        let tunnel1_pubkey = Pubkey::new_unique();
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
            owner: Pubkey::new_unique(),
        };

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Tunnel))
            .returning(move |_| {
                let mut tunnels = HashMap::new();
                tunnels.insert(tunnel1_pubkey, AccountData::Tunnel(tunnel1.clone()));
                Ok(tunnels)
            });

        let mut output = Vec::new();
        let res = ListTunnelArgs { code: None }.execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " pubkey                                    | code        | side_a       | side_z       | tunnel_type | bandwidth | mtu  | delay_ms | jitter_ms | tunnel_id | tunnel_net | status    | owner 
 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo | tunnel_code | device1_code | device2_code | MPLSoGRE    | 1.23Kbps  | 1566 |   0.00ms |    0.00ms | 1234      | 1.2.3.4/32 | activated | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9 
");
    }
}
