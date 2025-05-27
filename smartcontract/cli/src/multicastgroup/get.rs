use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::device::list::ListDeviceCommand;
use doublezero_sdk::commands::location::list::ListLocationCommand;
use doublezero_sdk::commands::multicastgroup::get::GetMulticastGroupCommand;
use doublezero_sdk::commands::user::list::ListUserCommand;
use doublezero_sdk::*;
use prettytable::{format, row, Cell, Row, Table};
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetMulticastGroupCliCommand {
    #[arg(long)]
    pub code: String,
}

impl GetMulticastGroupCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, mgroup) = client.get_multicastgroup(GetMulticastGroupCommand {
            pubkey_or_code: self.code,
        })?;

        let users = client.list_user(ListUserCommand {})?;
        let devices = client.list_device(ListDeviceCommand {})?;
        let locations = client.list_location(ListLocationCommand {})?;

        writeln!(out,
        "account: {}\r\ncode: {}\r\nmulticast_ip: {}\r\nmax_bandwidth: {}\r\rpublisher_allowlist: {}\r\nsubscriber_allowlist: {}\r\nstatus: {}\r\nowner: {}\r\n\r\nusers:\r\n",
        pubkey,
        mgroup.code,
        ipv4_to_string(&mgroup.multicast_ip),
        bandwidth_to_string(mgroup.max_bandwidth),
        mgroup.pub_allowlist.iter().map(|p| p.to_string()).collect::<Vec<_>>().join(", "),
        mgroup.sub_allowlist.iter().map(|p| p.to_string()).collect::<Vec<_>>().join(", "),
        mgroup.status,
        mgroup.owner
        )?;

        let mut table = Table::new();
        table.add_row(row![
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
            "owner"
        ]);

        for (pubkey, data) in users
            .into_iter()
            .filter(|(pk, _)| mgroup.publishers.contains(pk) || mgroup.subscribers.contains(pk))
        {
            let device = devices.get(&data.device_pk);
            let location = match device {
                Some(device) => locations.get(&device.location_pk),
                None => None,
            };

            let device_name = match device {
                Some(device) => device.code.clone(),
                None => data.device_pk.to_string(),
            };
            let location_name = match device {
                Some(device) => match location {
                    Some(location) => location.name.clone(),
                    None => device.location_pk.to_string(),
                },
                None => "".to_string(),
            };
            let mode_text = if mgroup.publishers.contains(&pubkey) {
                if !mgroup.subscribers.contains(&pubkey) {
                    "Tx"
                } else {
                    "Tx/Rx"
                }
            } else if mgroup.subscribers.contains(&pubkey) {
                "Rx"
            } else {
                "XX"
            };

            table.add_row(Row::new(vec![
                Cell::new(&pubkey.to_string()),
                Cell::new(&mode_text),
                Cell::new(&device_name),
                Cell::new(&location_name),
                Cell::new(&data.cyoa_type.to_string()),
                Cell::new(&ipv4_to_string(&data.client_ip)),
                Cell::new(&data.tunnel_id.to_string()),
                Cell::new(&networkv4_to_string(&data.tunnel_net)),
                Cell::new(&ipv4_to_string(&data.dz_ip)),
                Cell::new(&data.status.to_string()),
                Cell::new(&data.owner.to_string()),
            ]));
        }

        table.set_format(*format::consts::FORMAT_NO_BORDER_LINE_SEPARATOR);
        let _ = table.print(out);

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::doublezerocommand::CliCommand;
    use crate::multicastgroup::get::GetMulticastGroupCliCommand;
    use crate::tests::tests::create_test_client;
    use doublezero_sdk::commands::multicastgroup::get::GetMulticastGroupCommand;
    use doublezero_sdk::{
        get_multicastgroup_pda, AccountType, MulticastGroup, MulticastGroupStatus,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_multicastgroup_get() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_multicastgroup_pda(&client.get_program_id(), 1);

        let multicastgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            tenant_pk: Pubkey::new_unique(),
            multicast_ip: [10, 0, 0, 1],
            max_bandwidth: 1000000000,
            pub_allowlist: vec![],
            sub_allowlist: vec![],
            publishers: vec![],
            subscribers: vec![],
            status: MulticastGroupStatus::Activated,
            owner: pda_pubkey,
        };

        let multicastgroup2 = multicastgroup.clone();
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: pda_pubkey.to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, multicastgroup.clone())));
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: "test".to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, multicastgroup2.clone())));
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
        assert!(!res.is_ok(), "I shouldn't find anything.");

        // Expected success
        let mut output = Vec::new();
        let res = GetMulticastGroupCliCommand {
            code: pda_pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by pubkey");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: CahibSNzuzo1MZhonHXLw3bJRmZoecDSJRXWdH4WFYJK\r\ncode: test\r\nmulticast_ip: 10.0.0.1\r\nmax_bandwidth: 1Gbps\r\nstatus: activated\r\nowner: CahibSNzuzo1MZhonHXLw3bJRmZoecDSJRXWdH4WFYJK\n");

        // Expected success
        let mut output = Vec::new();
        let res = GetMulticastGroupCliCommand {
            code: "test".to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: CahibSNzuzo1MZhonHXLw3bJRmZoecDSJRXWdH4WFYJK\r\ncode: test\r\nmulticast_ip: 10.0.0.1\r\nmax_bandwidth: 1Gbps\r\nstatus: activated\r\nowner: CahibSNzuzo1MZhonHXLw3bJRmZoecDSJRXWdH4WFYJK\n");
    }
}
