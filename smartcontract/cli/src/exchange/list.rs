use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::{commands::exchange::list::ListExchangeCommand, *};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct ListExchangeCliCommand {
    /// Output in JSON format
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output in compact JSON format
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct ExchangeDisplay {
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    pub name: String,
    pub lat: f64,
    pub lng: f64,
    pub loc_id: u32,
    pub status: ExchangeStatus,
    #[serde(serialize_with = "crate::serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListExchangeCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let exchanges = client.list_exchange(ListExchangeCommand)?;

        let mut exchanges: Vec<(Pubkey, Exchange)> = exchanges.into_iter().collect();
        exchanges.sort_by(|(_, a), (_, b)| a.owner.cmp(&b.owner));

        let exchange_displays: Vec<ExchangeDisplay> = exchanges
            .into_iter()
            .map(|(pubkey, tunnel)| ExchangeDisplay {
                account: pubkey,
                code: tunnel.code,
                name: tunnel.name,
                lat: tunnel.lat,
                lng: tunnel.lng,
                loc_id: tunnel.loc_id,
                status: tunnel.status,
                owner: tunnel.owner,
            })
            .collect();

        let res = if self.json {
            serde_json::to_string_pretty(&exchange_displays)?
        } else if self.json_compact {
            serde_json::to_string(&exchange_displays)?
        } else {
            Table::new(exchange_displays)
                .with(Style::psql().remove_horizontals())
                .to_string()
        };

        writeln!(out, "{res}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        exchange::list::{ExchangeStatus::Activated, ListExchangeCliCommand},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{AccountType, Device, DeviceStatus, DeviceType, Exchange};
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    #[test]
    fn test_cli_exchange_list() {
        let mut client = create_test_client();

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
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
            contributor_pk,
            location_pk: location1_pubkey,
            exchange_pk: exchange1_pubkey,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4],
            dz_prefixes: vec![([1, 2, 3, 4], 32)],
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: Pubkey::new_unique(),
        };
        let device2_pubkey = Pubkey::new_unique();
        let device2 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            code: "device2_code".to_string(),
            contributor_pk,
            location_pk: location2_pubkey,
            exchange_pk: exchange2_pubkey,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4],
            dz_prefixes: vec![([1, 2, 3, 4], 32)],
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: Pubkey::new_unique(),
        };

        client.expect_list_device().returning(move |_| {
            let mut devices = HashMap::new();
            devices.insert(device1_pubkey, device1.clone());
            devices.insert(device2_pubkey, device2.clone());
            Ok(devices)
        });

        let exchange1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let exchange1 = Exchange {
            account_type: AccountType::Exchange,
            owner: exchange1_pubkey,
            index: 1,
            bump_seed: 2,
            lat: 15.00,
            lng: 15.00,
            loc_id: 6,
            status: Activated,
            code: "some code".to_string(),
            name: "some name".to_string(),
        };

        client.expect_list_exchange().returning(move |_| {
            let mut exchanges = HashMap::new();
            exchanges.insert(exchange1_pubkey, exchange1.clone());
            Ok(exchanges)
        });

        let mut output = Vec::new();
        let res = ListExchangeCliCommand {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | code      | name      | lat | lng | loc_id | status    | owner                                     \n 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo | some code | some name | 15  | 15  | 6      | activated | 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo \n");

        let mut output = Vec::new();
        let res = ListExchangeCliCommand {
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());

        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\",\"code\":\"some code\",\"name\":\"some name\",\"lat\":15.0,\"lng\":15.0,\"loc_id\":6,\"status\":\"Activated\",\"owner\":\"11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo\"}]\n");
    }
}
