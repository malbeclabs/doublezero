use crate::{doublezerocommand::CliCommand, validators::validate_code};
use clap::Args;
use doublezero_program_common::{serializer, types::parse_utils::bandwidth_to_string};
use doublezero_sdk::{
    commands::{
        contributor::get::GetContributorCommand, device::get::GetDeviceCommand,
        exchange::get::GetExchangeCommand,
    },
    GetLocationCommand, Interface,
};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

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
struct InterfaceDisplay {
    pub name: String,
    pub status: String,
    pub interface_type: String,
    pub cyoa: String,
    pub dia: String,
    pub loopback: String,
    pub ip_net: String,
    pub vlan_id: u16,
    pub bandwidth: String,
    #[tabled(skip)]
    pub bandwidth_bps: u64,
    pub cir: String,
    #[tabled(skip)]
    pub cir_bps: u64,
    pub mtu: u16,
    pub routing: String,
    pub seg_idx: u16,
    pub tunnel_endpoint: bool,
}

impl From<&Interface> for InterfaceDisplay {
    fn from(iface: &Interface) -> Self {
        // Convert to current version to ensure all fields are populated, even if the stored version is older
        let iface = iface.into_current_version();

        Self {
            name: iface.name.clone(),
            status: iface.status.to_string(),
            interface_type: iface.interface_type.to_string(),
            cyoa: iface.interface_cyoa.to_string(),
            dia: iface.interface_dia.to_string(),
            loopback: iface.loopback_type.to_string(),
            ip_net: iface.ip_net.to_string(),
            vlan_id: iface.vlan_id,
            bandwidth: bandwidth_to_string(&iface.bandwidth),
            bandwidth_bps: iface.bandwidth,
            cir: bandwidth_to_string(&iface.cir),
            cir_bps: iface.cir,
            mtu: iface.mtu,
            routing: iface.routing_mode.to_string(),
            seg_idx: iface.node_segment_idx,
            tunnel_endpoint: iface.user_tunnel_endpoint,
        }
    }
}

#[derive(Tabled, Serialize)]
struct DeviceDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub code: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(skip)]
    pub contributor_pk: Pubkey,
    pub contributor: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(skip)]
    pub location_pk: Pubkey,
    pub location: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(skip)]
    pub exchange_pk: Pubkey,
    pub exchange: String,
    pub device_type: String,
    pub public_ip: String,
    pub dz_prefixes: String,
    pub metrics_publisher: String,
    pub mgmt_vrf: String,
    #[tabled(skip)]
    pub interfaces: Vec<InterfaceDisplay>,
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
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl GetDeviceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, device) = client.get_device(GetDeviceCommand {
            pubkey_or_code: self.code,
        })?;

        let display = DeviceDisplay {
            account: pubkey,
            code: device.code,
            contributor_pk: device.contributor_pk,
            contributor: client
                .get_contributor(GetContributorCommand {
                    pubkey_or_code: device.contributor_pk.to_string(),
                })
                .map_or_else(|_| String::new(), |(_, c)| c.code),
            location_pk: device.location_pk,
            location: client
                .get_location(GetLocationCommand {
                    pubkey_or_code: device.location_pk.to_string(),
                })
                .map_or_else(|_| String::new(), |(_, l)| l.code),
            exchange_pk: device.exchange_pk,
            exchange: client
                .get_exchange(GetExchangeCommand {
                    pubkey_or_code: device.exchange_pk.to_string(),
                })
                .map_or_else(|_| String::new(), |(_, e)| e.code),
            device_type: device.device_type.to_string(),
            public_ip: device.public_ip.to_string(),
            dz_prefixes: device.dz_prefixes.to_string(),
            metrics_publisher: device.metrics_publisher_pk.to_string(),
            mgmt_vrf: device.mgmt_vrf,
            interfaces: device
                .interfaces
                .iter()
                .map(InterfaceDisplay::from)
                .collect(),
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
            owner: device.owner,
        };

        if self.json {
            let json = serde_json::to_string_pretty(&display)?;
            writeln!(out, "{json}")?;
        } else {
            let headers = DeviceDisplay::headers();
            let fields = display.fields();
            let max_len = headers.iter().map(|h| h.len()).max().unwrap_or(0);
            for (header, value) in headers.iter().zip(fields.iter()) {
                writeln!(out, " {header:<max_len$} | {value}")?;
            }
            if !display.interfaces.is_empty() {
                writeln!(out)?;
                let table = Table::new(&display.interfaces)
                    .with(Style::psql().remove_horizontals())
                    .to_string();
                writeln!(out, "{table}")?;
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{device::get::GetDeviceCliCommand, tests::utils::create_test_client};
    use doublezero_sdk::{
        commands::{
            contributor::get::GetContributorCommand, device::get::GetDeviceCommand,
            exchange::get::GetExchangeCommand,
        },
        AccountType, Contributor, ContributorStatus, Device, DeviceStatus, DeviceType, Exchange,
        ExchangeStatus, GetLocationCommand, Location, LocationStatus,
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

        let contributor = Contributor {
            account_type: AccountType::Contributor,
            index: 1,
            bump_seed: 255,
            code: "test-contributor".to_string(),
            reference_count: 0,
            status: ContributorStatus::Activated,
            owner: contributor_pk,
            ops_manager_pk: Pubkey::default(),
        };
        let location = Location {
            account_type: AccountType::Location,
            owner: Pubkey::default(),
            index: 1,
            bump_seed: 255,
            lat: 0.0,
            lng: 0.0,
            loc_id: 1,
            status: LocationStatus::Activated,
            code: "test-location".to_string(),
            name: "Test Location".to_string(),
            country: "US".to_string(),
            reference_count: 0,
        };
        let exchange = Exchange {
            account_type: AccountType::Exchange,
            owner: Pubkey::default(),
            index: 1,
            bump_seed: 255,
            lat: 0.0,
            lng: 0.0,
            bgp_community: 100,
            unused: 0,
            status: ExchangeStatus::Activated,
            code: "test-exchange".to_string(),
            name: "Test Exchange".to_string(),
            reference_count: 0,
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
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
        client
            .expect_get_contributor()
            .with(predicate::eq(GetContributorCommand {
                pubkey_or_code: contributor_pk.to_string(),
            }))
            .returning(move |_| Ok((contributor_pk, contributor.clone())));
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
        let has_row = |header: &str, value: &str| {
            output_str
                .lines()
                .any(|l| l.contains(header) && l.contains(value))
        };
        assert!(
            has_row("account", &device1_pubkey.to_string()),
            "account row should contain pubkey"
        );
        assert!(has_row("code", "test"), "code row should contain value");
        assert!(
            has_row("status", "activated"),
            "status row should contain value"
        );

        // Expected success by pubkey (JSON)
        let mut output = Vec::new();
        let res = GetDeviceCliCommand {
            code: device1_pubkey.to_string(),
            json: true,
        }
        .execute(&client, &mut output);
        assert!(
            res.is_ok(),
            "I should find a device by pubkey with JSON output"
        );
        let json: serde_json::Value =
            serde_json::from_str(&String::from_utf8(output).unwrap()).unwrap();
        assert_eq!(
            json["account"].as_str().unwrap(),
            device1_pubkey.to_string()
        );
        assert_eq!(json["code"].as_str().unwrap(), "test");
        assert_eq!(json["status"].as_str().unwrap(), "activated");
        assert_eq!(json["contributor"].as_str().unwrap(), "test-contributor");
        assert_eq!(json["location"].as_str().unwrap(), "test-location");
        assert_eq!(json["exchange"].as_str().unwrap(), "test-exchange");
        assert_eq!(json["interfaces"].as_array().unwrap().len(), 0);
    }
}
