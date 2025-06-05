use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::link::get::GetLinkCommand;
use doublezero_sdk::networkv4_to_string;
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetLinkCliCommand {
    #[arg(long)]
    pub code: String,
}

impl GetLinkCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, tunnel) = client.get_link(GetLinkCommand {
            pubkey_or_code: self.code,
        })?;

        writeln!(out,
            "account: {}\r\ncode: {}\r\nside_a: {}\r\nside_z: {}\r\ntunnel_type: {}\r\nbandwidth: {}\r\nmtu: {}\r\ndelay: {}ms\r\njitter: {}ms\r\ntunnel_net: {}\r\nstatus: {}\r\nowner: {}",
            pubkey,
            tunnel.code,
            tunnel.side_a_pk,
            tunnel.side_z_pk,
            tunnel.link_type,
            tunnel.bandwidth,
            tunnel.mtu,
            tunnel.delay_ns as f32 / 1000000.0,
            tunnel.jitter_ns as f32 / 1000000.0,
            networkv4_to_string(&tunnel.tunnel_net),
            tunnel.status,
            tunnel.owner
            )?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::doublezerocommand::CliCommand;
    use crate::link::get::GetLinkCliCommand;
    use crate::tests::utils::create_test_client;
    use doublezero_sdk::commands::link::get::GetLinkCommand;
    use doublezero_sdk::{get_link_pda, AccountType, Link, LinkLinkType, LinkStatus};
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_link_get() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_link_pda(&client.get_program_id(), 1);
        let device1_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcb");
        let device2_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcf");

        let tunnel = Link {
            account_type: AccountType::Link,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            side_a_pk: device1_pk,
            side_z_pk: device2_pk,
            link_type: LinkLinkType::L3,
            bandwidth: 1000000000,
            mtu: 1500,
            delay_ns: 10000000000,
            jitter_ns: 5000000000,
            tunnel_id: 1,
            tunnel_net: ([10, 0, 0, 1], 16),
            status: LinkStatus::Activated,
            owner: pda_pubkey,
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
        assert_eq!(output_str, "account: 45oivwjiumVv8uwsJw8qPjG3EQy9Yn2qAuqLzA5XoE1Q\r\ncode: test\r\nside_a: HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcb\r\nside_z: HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcf\r\ntunnel_type: L3\r\nbandwidth: 1000000000\r\nmtu: 1500\r\ndelay: 10000ms\r\njitter: 5000ms\r\ntunnel_net: 10.0.0.1/16\r\nstatus: activated\r\nowner: 45oivwjiumVv8uwsJw8qPjG3EQy9Yn2qAuqLzA5XoE1Q\n");

        // Expected success
        let mut output = Vec::new();
        let res = GetLinkCliCommand {
            code: "test".to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: 45oivwjiumVv8uwsJw8qPjG3EQy9Yn2qAuqLzA5XoE1Q\r\ncode: test\r\nside_a: HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcb\r\nside_z: HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcf\r\ntunnel_type: L3\r\nbandwidth: 1000000000\r\nmtu: 1500\r\ndelay: 10000ms\r\njitter: 5000ms\r\ntunnel_net: 10.0.0.1/16\r\nstatus: activated\r\nowner: 45oivwjiumVv8uwsJw8qPjG3EQy9Yn2qAuqLzA5XoE1Q\n");
    }
}
