use crate::{doublezerocommand::CliCommand, validators::validate_code};
use clap::Args;
use doublezero_program_common::serializer;
use doublezero_sdk::commands::link::get::GetLinkCommand;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct GetLinkCliCommand {
    /// The pubkey or code of the link to retrieve
    #[arg(long, value_parser = validate_code)]
    pub code: String,
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
}

#[derive(Tabled, Serialize)]
struct LinkDisplay {
    pub account: String,
    pub code: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(skip)]
    pub contributor_pk: Pubkey,
    pub contributor: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(skip)]
    pub side_a_pk: Pubkey,
    pub side_a: String,
    pub side_a_iface_name: String,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    #[tabled(skip)]
    pub side_z_pk: Pubkey,
    pub side_z: String,
    pub side_z_iface_name: String,
    pub tunnel_type: String,
    pub bandwidth: u64,
    pub mtu: u32,
    pub delay: String,
    pub jitter: String,
    pub delay_override: String,
    pub tunnel_id: u16,
    pub tunnel_net: String,
    pub desired_status: String,
    pub status: String,
    pub health: String,
    pub owner: String,
}

impl GetLinkCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, link) = client.get_link(GetLinkCommand {
            pubkey_or_code: self.code,
        })?;

        let display = LinkDisplay {
            account: pubkey.to_string(),
            code: link.code,
            contributor_pk: link.contributor_pk,
            contributor: client
                .get_contributor(
                    doublezero_sdk::commands::contributor::get::GetContributorCommand {
                        pubkey_or_code: link.contributor_pk.to_string(),
                    },
                )
                .map_or_else(|_| String::new(), |(_, c)| c.code),
            side_a_pk: link.side_a_pk,
            side_a: client
                .get_device(doublezero_sdk::commands::device::get::GetDeviceCommand {
                    pubkey_or_code: link.side_a_pk.to_string(),
                })
                .map_or_else(|_| String::new(), |(_, d)| d.code),
            side_a_iface_name: link.side_a_iface_name,
            side_z_pk: link.side_z_pk,
            side_z: client
                .get_device(doublezero_sdk::commands::device::get::GetDeviceCommand {
                    pubkey_or_code: link.side_z_pk.to_string(),
                })
                .map_or_else(|_| String::new(), |(_, d)| d.code),
            side_z_iface_name: link.side_z_iface_name,
            tunnel_type: link.link_type.to_string(),
            bandwidth: link.bandwidth,
            mtu: link.mtu,
            delay: format!("{}ms", link.delay_ns as f32 / 1_000_000.0),
            jitter: format!("{}ms", link.jitter_ns as f32 / 1_000_000.0),
            delay_override: format!("{}ms", link.delay_override_ns as f32 / 1_000_000.0),
            tunnel_id: link.tunnel_id,
            tunnel_net: link.tunnel_net.to_string(),
            desired_status: link.desired_status.to_string(),
            status: link.status.to_string(),
            health: link.link_health.to_string(),
            owner: link.owner.to_string(),
        };

        if self.json {
            let json = serde_json::to_string_pretty(&display)?;
            writeln!(out, "{json}")?;
        } else {
            let headers = LinkDisplay::headers();
            let fields = display.fields();
            let max_len = headers.iter().map(|h| h.len()).max().unwrap_or(0);
            for (header, value) in headers.iter().zip(fields.iter()) {
                writeln!(out, " {header:<max_len$} | {value}")?;
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand, link::get::GetLinkCliCommand,
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::{
            contributor::get::GetContributorCommand, device::get::GetDeviceCommand,
            link::get::GetLinkCommand,
        },
        get_link_pda, AccountType, Contributor, ContributorStatus, Device, DeviceStatus,
        DeviceType, Link, LinkLinkType, LinkStatus,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_link_get() {
        let mut client = create_test_client();

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let (pda_pubkey, _bump_seed) = get_link_pda(&client.get_program_id(), 1);
        let device1_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcb");
        let device2_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcf");

        let tunnel = Link {
            account_type: AccountType::Link,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            contributor_pk,
            side_a_pk: device1_pk,
            side_z_pk: device2_pk,
            link_type: LinkLinkType::WAN,
            bandwidth: 1000000000,
            mtu: 1500,
            delay_ns: 10000000000,
            jitter_ns: 5000000000,
            delay_override_ns: 0,
            tunnel_id: 1,
            tunnel_net: "10.0.0.1/16".parse().unwrap(),
            status: LinkStatus::Activated,
            owner: pda_pubkey,
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
            link_health: doublezero_serviceability::state::link::LinkHealth::ReadyForService,
            desired_status: doublezero_serviceability::state::link::LinkDesiredStatus::Activated,
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
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 255,
            reference_count: 0,
            code: "side-a-device".to_string(),
            contributor_pk,
            location_pk: Pubkey::default(),
            exchange_pk: Pubkey::default(),
            device_type: DeviceType::Hybrid,
            public_ip: [1, 2, 3, 4].into(),
            dz_prefixes: "1.2.3.4/32".parse().unwrap(),
            status: DeviceStatus::Activated,
            metrics_publisher_pk: Pubkey::default(),
            owner: device1_pk,
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
        let device2 = Device {
            code: "side-z-device".to_string(),
            owner: device2_pk,
            ..device1.clone()
        };

        let tunnel2 = tunnel.clone();
        client
            .expect_get_link()
            .with(predicate::eq(GetLinkCommand {
                pubkey_or_code: pda_pubkey.to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, tunnel.clone())));
        client
            .expect_get_link()
            .with(predicate::eq(GetLinkCommand {
                pubkey_or_code: "test".to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, tunnel2.clone())));
        client
            .expect_get_link()
            .returning(move |_| Err(eyre::eyre!("not found")));
        client
            .expect_get_contributor()
            .with(predicate::eq(GetContributorCommand {
                pubkey_or_code: contributor_pk.to_string(),
            }))
            .returning(move |_| Ok((contributor_pk, contributor.clone())));
        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device1_pk.to_string(),
            }))
            .returning(move |_| Ok((device1_pk, device1.clone())));
        client
            .expect_get_device()
            .with(predicate::eq(GetDeviceCommand {
                pubkey_or_code: device2_pk.to_string(),
            }))
            .returning(move |_| Ok((device2_pk, device2.clone())));

        // Expected failure
        let mut output = Vec::new();
        let res = GetLinkCliCommand {
            code: Pubkey::new_unique().to_string(),
            json: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_err(), "I shouldn't find anything.");

        // Expected success by pubkey (table)
        let mut output = Vec::new();
        let res = GetLinkCliCommand {
            code: pda_pubkey.to_string(),
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
            has_row("account", &pda_pubkey.to_string()),
            "account row should contain pubkey"
        );
        assert!(has_row("code", "test"), "code row should contain value");
        assert!(
            has_row("tunnel_type", "WAN"),
            "tunnel_type row should contain value"
        );
        assert!(
            has_row("status", "activated"),
            "status row should contain value"
        );

        // Expected success by code (JSON)
        let mut output = Vec::new();
        let res = GetLinkCliCommand {
            code: "test".to_string(),
            json: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
        let json: serde_json::Value =
            serde_json::from_str(&String::from_utf8(output).unwrap()).unwrap();
        assert_eq!(json["account"].as_str().unwrap(), pda_pubkey.to_string());
        assert_eq!(json["code"].as_str().unwrap(), "test");
        assert_eq!(json["status"].as_str().unwrap(), "activated");
        assert_eq!(json["tunnel_type"].as_str().unwrap(), "WAN");
        assert_eq!(json["bandwidth"].as_u64().unwrap(), 1_000_000_000);
        assert_eq!(json["mtu"].as_u64().unwrap(), 1500);
        assert_eq!(json["contributor"].as_str().unwrap(), "test-contributor");
        assert_eq!(json["side_a"].as_str().unwrap(), "side-a-device");
        assert_eq!(json["side_z"].as_str().unwrap(), "side-z-device");
    }
}
