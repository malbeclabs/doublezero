use crate::{doublezerocommand::CliCommand, validators::validate_code};
use clap::Args;
use doublezero_sdk::commands::device::get::GetDeviceCommand;
use serde::Serialize;
use std::io::Write;
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct GetDeviceCliCommand {
    /// Device Pubkey or code to retrieve
    #[arg(long, value_parser = validate_code)]
    pub code: String,
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
}

#[derive(Tabled, Serialize)]
struct DeviceDisplay {
    pub account: String,
    pub code: String,
    pub contributor: String,
    pub location: String,
    pub exchange: String,
    pub device_type: String,
    pub public_ip: String,
    pub dz_prefixes: String,
    pub metrics_publisher: String,
    pub mgmt_vrf: String,
    pub interfaces: String,
    pub max_users: u16,
    pub users_count: u16,
    pub reference_count: u32,
    pub max_unicast_users: u16,
    pub unicast_users_count: u16,
    pub max_multicast_users: u16,
    pub multicast_users_count: u16,
    pub desired_status: String,
    pub status: String,
    pub health: String,
    pub owner: String,
}

impl GetDeviceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, device) = client.get_device(GetDeviceCommand {
            pubkey_or_code: self.code,
        })?;

        let display = DeviceDisplay {
            account: pubkey.to_string(),
            code: device.code,
            contributor: device.contributor_pk.to_string(),
            location: device.location_pk.to_string(),
            exchange: device.exchange_pk.to_string(),
            device_type: device.device_type.to_string(),
            public_ip: device.public_ip.to_string(),
            dz_prefixes: device.dz_prefixes.to_string(),
            metrics_publisher: device.metrics_publisher_pk.to_string(),
            mgmt_vrf: device.mgmt_vrf,
            interfaces: format!("{:?}", device.interfaces),
            max_users: device.max_users,
            users_count: device.users_count,
            reference_count: device.reference_count,
            max_unicast_users: device.max_unicast_users,
            unicast_users_count: device.unicast_users_count,
            max_multicast_users: device.max_multicast_users,
            multicast_users_count: device.multicast_users_count,
            desired_status: device.desired_status.to_string(),
            status: device.status.to_string(),
            health: device.device_health.to_string(),
            owner: device.owner.to_string(),
        };

        if self.json {
            let json = serde_json::to_string_pretty(&display)?;
            writeln!(out, "{json}")?;
        } else {
            let table = tabled::Table::new([display]);
            writeln!(out, "{table}")?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{device::get::GetDeviceCliCommand, tests::utils::create_test_client};
    use doublezero_sdk::{
        commands::device::get::GetDeviceCommand, AccountType, Device, DeviceStatus, DeviceType,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use std::str::FromStr;

    #[test]
    fn test_cli_device_get() {
        let mut client = create_test_client();

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let location_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let exchange_pk = Pubkey::from_str_const("GQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcc");
        let device1_pubkey =
            Pubkey::from_str("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB").unwrap();
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "test".to_string(),
            contributor_pk,
            location_pk,
            exchange_pk,
            device_type: DeviceType::Hybrid,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::from_str_const(
                "1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPR",
            ),
            owner: device1_pubkey,
            mgmt_vrf: "default".to_string(),
            interfaces: vec![],
            max_users: 255,
            users_count: 0,
            device_health: doublezero_serviceability::state::device::DeviceHealth::ReadyForUsers,
            desired_status:
                doublezero_serviceability::state::device::DeviceDesiredStatus::Activated,
            unicast_users_count: 0,
            multicast_users_count: 0,
            max_unicast_users: 0,
            max_multicast_users: 0,
            reserved_seats: 0,
        };

        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device1_pubkey.to_string(),
            }))
            .returning(move |_| Ok((device1_pubkey, device1.clone())));
        client
            .expect_get_device()
            .returning(move |_| Err(eyre::eyre!("not found")));

        // Expected failure
        let mut output = Vec::new();
        let res = GetDeviceCliCommand {
            code: Pubkey::new_unique().to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_err(), "I shouldn't find anything.");

        // Expected success (table)
        let mut output = Vec::new();
        let res = GetDeviceCliCommand {
            code: device1_pubkey.to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by pubkey");
        let output_str = String::from_utf8(output).unwrap();
        assert!(
            output_str.contains("account"),
            "should contain table header"
        );
        assert!(
            output_str.contains("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB"),
            "should contain pubkey"
        );
        assert!(output_str.contains("test"), "should contain code");
        assert!(output_str.contains("activated"), "should contain status");
    }
}
