use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::multicastgroup::list::ListMulticastGroupCommand;
use doublezero_sdk::*;
use prettytable::{format, row, Cell, Row, Table};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
pub struct ListMulticastGroupCliCommand {
    #[arg(long, default_value_t = false)]
    pub json: bool,
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Serialize)]
pub struct MulticastGroupDisplay {
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
    #[serde(serialize_with = "crate::serializer::serialize_ipv4_as_string")]
    pub multicast_ip: IpV4,
    #[serde(serialize_with = "crate::serializer::serialize_bandwidth_as_string")]
    pub max_bandwidth: u64,
    #[serde(serialize_with = "crate::serializer::serialize_pubkeylist_as_string")]
    pub publishers: Vec<Pubkey>,
    #[serde(serialize_with = "crate::serializer::serialize_pubkeylist_as_string")]
    pub subscribers: Vec<Pubkey>,
    pub status: MulticastGroupStatus,
}

impl ListMulticastGroupCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let multicastgroups = client.list_multicastgroup(ListMulticastGroupCommand {})?;

        let mut multicastgroups: Vec<(Pubkey, MulticastGroup)> =
            multicastgroups.into_iter().collect();
        multicastgroups.sort_by(|(_, a), (_, b)| a.owner.cmp(&b.owner));

        if self.json || self.json_compact {
            let multicastgroups = multicastgroups
                .into_iter()
                .map(|(pubkey, multicastgroup)| MulticastGroupDisplay {
                    account: pubkey,
                    code: multicastgroup.code,
                    owner: multicastgroup.owner,
                    multicast_ip: multicastgroup.multicast_ip,
                    max_bandwidth: multicastgroup.max_bandwidth,
                    publishers: multicastgroup.publishers,
                    subscribers: multicastgroup.subscribers,
                    status: multicastgroup.status,
                })
                .collect::<Vec<_>>();

            let json = {
                if self.json_compact {
                    serde_json::to_string(&multicastgroups)?
                } else {
                    serde_json::to_string_pretty(&multicastgroups)?
                }
            };
            writeln!(out, "{}", json)?;
        } else {
            let mut table = Table::new();
            table.add_row(row![
                "account",
                "code",
                "multicast_ip",
                "max_bandwidth",
                "publishers",
                "subscribers",
                "status",
                "owner"
            ]);
            for (pubkey, data) in multicastgroups {
                table.add_row(Row::new(vec![
                    Cell::new(&pubkey.to_string()),
                    Cell::new(&data.code),
                    Cell::new(&ipv4_to_string(&data.multicast_ip)),
                    Cell::new(&bandwidth_to_string(data.max_bandwidth)),
                    Cell::new(&data.publishers.len().to_string()),
                    Cell::new(&data.subscribers.len().to_string()),
                    Cell::new(&data.status.to_string()),
                    Cell::new(&data.owner.to_string()),
                ]));
            }

            table.set_format(*format::consts::FORMAT_NO_BORDER_LINE_SEPARATOR);
            table.print(out)?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::multicastgroup::list::ListMulticastGroupCliCommand;
    use crate::tests::tests::create_test_client;
    use doublezero_sdk::{Device, DeviceStatus, DeviceType, MulticastGroup, MulticastGroupStatus};
    use doublezero_sla_program::state::accounttype::AccountType;
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    #[test]
    fn test_cli_multicastgroup_list() {
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

        let multicastgroup1_pubkey =
            Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR");
        let multicastgroup1 = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 2,
            tenant_pk: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
            code: "multicastgroup_code".to_string(),
            multicast_ip: [1, 2, 3, 4],
            max_bandwidth: 1234,
            pub_allowlist: vec![],
            sub_allowlist: vec![],
            publishers: vec![
                Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo2"),
                Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo3"),
            ],
            subscribers: vec![Pubkey::from_str_const(
                "11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo3",
            )],
            status: MulticastGroupStatus::Activated,
            owner: Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9"),
        };

        client.expect_list_multicastgroup().returning(move |_| {
            let mut multicastgroups = HashMap::new();
            multicastgroups.insert(multicastgroup1_pubkey, multicastgroup1.clone());
            Ok(multicastgroups)
        });

        let mut output = Vec::new();
        let res = ListMulticastGroupCliCommand {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | code                | multicast_ip | max_bandwidth | publishers | subscribers | status    | owner \n 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR | multicastgroup_code | 1.2.3.4      | 1.23Kbps      | 2          | 1           | activated | 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9 \n");

        let mut output = Vec::new();
        let res = ListMulticastGroupCliCommand {
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR\",\"code\":\"multicastgroup_code\",\"owner\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo9\",\"multicast_ip\":\"1.2.3.4\",\"max_bandwidth\":\"1.23Kbps\",\"publishers\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo2, 11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo3\",\"subscribers\":\"11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo3\",\"status\":\"Activated\"}]\n");
    }
}
