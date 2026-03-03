use crate::{doublezerocommand::CliCommand, validators::validate_code};
use clap::Args;
use doublezero_sdk::commands::link::get::GetLinkCommand;
use serde::Serialize;
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
    pub contributor: String,
    pub side_a: String,
    pub side_a_iface_name: String,
    pub side_z: String,
    pub side_z_iface_name: String,
    pub tunnel_type: String,
    pub bandwidth: u64,
    pub mtu: u32,
    pub delay: String,
    pub jitter: String,
    pub delay_override: String,
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
            contributor: link.contributor_pk.to_string(),
            side_a: link.side_a_pk.to_string(),
            side_a_iface_name: link.side_a_iface_name,
            side_z: link.side_z_pk.to_string(),
            side_z_iface_name: link.side_z_iface_name,
            tunnel_type: link.link_type.to_string(),
            bandwidth: link.bandwidth,
            mtu: link.mtu,
            delay: format!("{}ms", link.delay_ns as f32 / 1_000_000.0),
            jitter: format!("{}ms", link.jitter_ns as f32 / 1_000_000.0),
            delay_override: format!("{}ms", link.delay_override_ns as f32 / 1_000_000.0),
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
            let table = tabled::Table::new([display]);
            writeln!(out, "{table}")?;
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
        commands::link::get::GetLinkCommand, get_link_pda, AccountType, Link, LinkLinkType,
        LinkStatus,
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
        assert!(
            output_str.contains("account"),
            "should contain table header"
        );
        assert!(output_str.contains("test"), "should contain code");
        assert!(output_str.contains("WAN"), "should contain tunnel type");
        assert!(output_str.contains("activated"), "should contain status");

        // Expected success by code (JSON)
        let mut output = Vec::new();
        let res = GetLinkCliCommand {
            code: "test".to_string(),
            json: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
        let output_str = String::from_utf8(output).unwrap();
        assert!(
            output_str.contains("\"account\""),
            "should contain account key"
        );
        assert!(output_str.contains("\"code\""), "should contain code key");
        assert!(output_str.contains("\"test\""), "should contain code value");
    }
}
