use crate::{
    doublezerocommand::CliCommand,
    helpers::parse_pubkey,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::{
        validate_code, validate_parse_ipv4, validate_parse_networkv4_list, validate_pubkey,
        validate_pubkey_or_code,
    },
};
use clap::Args;
use doublezero_sdk::{
    commands::{
        device::{create::CreateDeviceCommand, list::ListDeviceCommand},
        exchange::get::GetExchangeCommand,
        location::get::GetLocationCommand,
    },
    *,
};
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, str::FromStr};

#[derive(Args, Debug)]
pub struct CreateDeviceCliCommand {
    /// Unique device code
    #[arg(long, value_parser = validate_code)]
    pub code: String,
    /// Location (pubkey or code) associated with the device
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub location: String,
    /// Exchange (pubkey or code) associated with the device
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub exchange: String,
    /// Device public IPv4 address (e.g. 10.0.0.1)
    #[arg(long, value_parser = validate_parse_ipv4)]
    pub public_ip: IpV4,
    /// List of DZ prefixes in comma-separated CIDR format (e.g. 10.1.0.0/16,10.2.0.0/16)
    #[arg(long, value_parser = validate_parse_networkv4_list)]
    pub dz_prefixes: NetworkV4List,
    /// Metrics publisher public key (optional, defaults to zeroed pubkey)
    #[arg(long, value_parser = validate_pubkey)]
    pub metrics_publisher: Option<String>,
}

impl CreateDeviceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let devices = client.list_device(ListDeviceCommand {})?;
        if devices.iter().any(|(_, d)| d.code == self.code) {
            return Err(eyre::eyre!(
                "Device with code '{}' already exists",
                self.code
            ));
        }
        if devices.iter().any(|(_, d)| d.public_ip == self.public_ip) {
            return Err(eyre::eyre!(
                "Device with public ip '{}' already exists",
                ipv4_to_string(&self.public_ip)
            ));
        }

        let location_pk = match parse_pubkey(&self.location) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = client
                    .get_location(GetLocationCommand {
                        pubkey_or_code: self.location.clone(),
                    })
                    .map_err(|_| eyre::eyre!("Location not found"))?;
                pubkey
            }
        };

        let exchange_pk = match parse_pubkey(&self.exchange) {
            Some(pk) => pk,
            None => {
                let (pubkey, _) = client
                    .get_exchange(GetExchangeCommand {
                        pubkey_or_code: self.exchange.clone(),
                    })
                    .map_err(|_| eyre::eyre!("Exchange not found"))?;
                pubkey
            }
        };

        let metrics_publisher = if let Some(metrics_publisher) = &self.metrics_publisher {
            if metrics_publisher == "me" {
                client.get_payer()
            } else {
                match Pubkey::from_str(metrics_publisher) {
                    Ok(pk) => pk,
                    Err(_) => return Err(eyre::eyre!("Invalid metrics publisher Pubkey")),
                }
            }
        } else {
            client.get_payer()
        };

        let (signature, _pubkey) = client.create_device(CreateDeviceCommand {
            code: self.code.clone(),
            location_pk,
            exchange_pk,
            device_type: DeviceType::Switch,
            public_ip: self.public_ip,
            dz_prefixes: self.dz_prefixes,
            metrics_publisher: metrics_publisher,
        })?;
        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{
        device::create::CreateDeviceCliCommand,
        doublezerocommand::CliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::{
            device::{create::CreateDeviceCommand, list::ListDeviceCommand},
            exchange::get::GetExchangeCommand,
        },
        get_device_pda, AccountType, DeviceType, Exchange, ExchangeStatus, GetLocationCommand,
        Location, LocationStatus,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_device_create() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_device_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let location_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let location = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            name: "Test Location".to_string(),
            country: "Test Country".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: 0,
            status: LocationStatus::Activated,
            owner: location_pk,
        };
        let exchange_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let exchange = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            name: "Test Exchange".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: 0,
            status: ExchangeStatus::Activated,
            owner: exchange_pk,
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_location()
            .with(predicate::eq(GetLocationCommand {
                pubkey_or_code: location_pk.to_string(),
            }))
            .returning(move |_| Ok((location_pk, location.clone())));
        client
            .expect_get_exchange()
            .with(predicate::eq(GetExchangeCommand {
                pubkey_or_code: exchange_pk.to_string(),
            }))
            .returning(move |_| Ok((exchange_pk, exchange.clone())));
        client
            .expect_list_device()
            .with(predicate::eq(ListDeviceCommand {}))
            .returning(move |_| Ok(HashMap::new()));
        client
            .expect_create_device()
            .with(predicate::eq(CreateDeviceCommand {
                code: "test".to_string(),
                location_pk,
                exchange_pk,
                device_type: DeviceType::Switch,
                public_ip: [100, 0, 0, 1],
                dz_prefixes: vec![([10, 1, 0, 0], 16)],
                metrics_publisher: Pubkey::default(),
            }))
            .returning(move |_| Ok((signature, pda_pubkey)));

        let mut output = Vec::new();
        let res = CreateDeviceCliCommand {
            code: "test".to_string(),
            location: location_pk.to_string(),
            exchange: exchange_pk.to_string(),
            public_ip: [100, 0, 0, 1],
            dz_prefixes: vec![([10, 1, 0, 0], 16)],
            metrics_publisher: Some(Pubkey::default().to_string()),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
