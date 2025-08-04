use crate::{doublezerocommand::CliCommand, validators::validate_code};
use clap::Args;
use doublezero_sdk::commands::link::get::GetLinkCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetLinkCliCommand {
    /// The pubkey or code of the link to retrieve
    #[arg(long, value_parser = validate_code)]
    pub code: String,
}

impl GetLinkCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, link) = client.get_link(GetLinkCommand {
            pubkey_or_code: self.code,
        })?;

        writeln!(
            out,
            "account: {}\r\n\
code: {}\r\n\
contributor: {}\r\n\
side_a: {}\r\n\
side_a_iface_name: {}\r\n\
side_z: {}\r\n\
side_z_iface_name: {}\r\n\
tunnel_type: {}\r\n\
bandwidth: {}\r\n\
mtu: {}\r\n\
delay: {}ms\r\n\
jitter: {}ms\r\n\
tunnel_net: {}\r\n\
status: {}\r\n\
owner: {}",
            pubkey,
            link.code,
            link.contributor_pk,
            link.side_a_pk,
            link.side_a_iface_name,
            link.side_z_pk,
            link.side_z_iface_name,
            link.link_type,
            link.bandwidth,
            link.mtu,
            link.delay_ns as f32 / 1000000.0,
            link.jitter_ns as f32 / 1000000.0,
            link.tunnel_net,
            link.status,
            link.owner
        )?;

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
            tunnel_id: 1,
            tunnel_net: "10.0.0.1/16".parse().unwrap(),
            status: LinkStatus::Activated,
            owner: pda_pubkey,
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
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
        /*****************************************************************************************************/
        // Expected failure
        let mut output = Vec::new();
        let res = GetLinkCliCommand {
            code: Pubkey::new_unique().to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_err(), "I shouldn't find anything.");

        // Expected success
        let mut output = Vec::new();
        let res = GetLinkCliCommand {
            code: pda_pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by pubkey");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: 313hjD3qvP9CCxdbTGKpuACJrBwh8DhXjdVoL6gc6rf9\r\ncode: test\r\ncontributor: HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx\r\nside_a: HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcb\r\nside_a_iface_name: eth0\r\nside_z: HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcf\r\nside_z_iface_name: eth1\r\ntunnel_type: WAN\r\nbandwidth: 1000000000\r\nmtu: 1500\r\ndelay: 10000ms\r\njitter: 5000ms\r\ntunnel_net: 10.0.0.1/16\r\nstatus: activated\r\nowner: 313hjD3qvP9CCxdbTGKpuACJrBwh8DhXjdVoL6gc6rf9\n");

        // Expected success
        let mut output = Vec::new();
        let res = GetLinkCliCommand {
            code: "test".to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: 313hjD3qvP9CCxdbTGKpuACJrBwh8DhXjdVoL6gc6rf9\r\ncode: test\r\ncontributor: HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx\r\nside_a: HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcb\r\nside_a_iface_name: eth0\r\nside_z: HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcf\r\nside_z_iface_name: eth1\r\ntunnel_type: WAN\r\nbandwidth: 1000000000\r\nmtu: 1500\r\ndelay: 10000ms\r\njitter: 5000ms\r\ntunnel_net: 10.0.0.1/16\r\nstatus: activated\r\nowner: 313hjD3qvP9CCxdbTGKpuACJrBwh8DhXjdVoL6gc6rf9\n");
    }
}
