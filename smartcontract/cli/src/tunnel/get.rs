use clap::Args;
use doublezero_sdk::commands::tunnel::get::GetTunnelCommand;
use doublezero_sdk::*;
use std::io::Write;
use crate::doublezerocommand::CliCommand;

#[derive(Args, Debug)]
pub struct GetTunnelCliCommand {
    #[arg(long)]
    pub code: String,
}

impl GetTunnelCliCommand {
    pub fn execute<W: Write>(self, client: &dyn CliCommand, out: &mut W) -> eyre::Result<()> {
        let (pubkey, tunnel) = client.get_tunnel(GetTunnelCommand {
            pubkey_or_code: self.code,
        })?;

        writeln!(out, 
            "account: {}\r\ncode: {}\r\nside_a: {}\r\nside_z: {}\r\ntunnel_type: {}\r\nbandwidth: {}\r\nmtu: {}\r\ndelay: {}ms\r\njitter: {}ms\r\ntunnel_net: {}\r\nstatus: {}\r\nowner: {}",
            pubkey,
            tunnel.code,
            tunnel.side_a_pk,
            tunnel.side_z_pk,
            tunnel.tunnel_type,
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
    use doublezero_sdk::commands::tunnel::get::GetTunnelCommand;
    use doublezero_sdk::{get_tunnel_pda, AccountType, Tunnel, TunnelStatus, TunnelTunnelType};
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use crate::doublezerocommand::CliCommand;
    use crate::tunnel::get::GetTunnelCliCommand;
    use crate::tests::tests::create_test_client;

    #[test]
    fn test_cli_tunnel_get() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_tunnel_pda(&client.get_program_id(), 1);
        let device1_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcb");
        let device2_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcf");

        let tunnel = Tunnel {
            account_type: AccountType::Tunnel,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            side_a_pk: device1_pk,
            side_z_pk: device2_pk,
            tunnel_type: TunnelTunnelType::MPLSoGRE,
            bandwidth: 1000000000,
            mtu: 1500,
            delay_ns: 10000000000,
            jitter_ns: 5000000000,
            tunnel_id: 1,
            tunnel_net: ([10, 0, 0, 1], 16),
            status: TunnelStatus::Activated,
            owner: pda_pubkey,
        };

        let tunnel2 = tunnel.clone();
        client
            .expect_get_tunnel()
            .with(predicate::eq(GetTunnelCommand {
                pubkey_or_code: pda_pubkey.to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, tunnel.clone())));
        client
            .expect_get_tunnel()
            .with(predicate::eq(GetTunnelCommand {
                pubkey_or_code: "test".to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, tunnel2.clone())));
        client
            .expect_get_tunnel()
            .returning(move |_| Err(eyre::eyre!("not found")));
        /*****************************************************************************************************/
        // Expected failure
        let mut output = Vec::new();
        let res = GetTunnelCliCommand {
            code: Pubkey::new_unique().to_string(),
        }
        .execute(&client, &mut output);
        assert!(!res.is_ok(), "I shouldn't find anything.");

        // Expected success
        let mut output = Vec::new();
        let res = GetTunnelCliCommand {
            code: pda_pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by pubkey");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: 8vB4Pa1eGbQ2GpAvbpZsykdmc5mrwYCXYboyRopB2Ev1\r\ncode: test\r\nside_a: HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcb\r\nside_z: HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcf\r\ntunnel_type: MPLSoGRE\r\nbandwidth: 1000000000\r\nmtu: 1500\r\ndelay: 10000ms\r\njitter: 5000ms\r\ntunnel_net: 10.0.0.1/16\r\nstatus: activated\r\nowner: 8vB4Pa1eGbQ2GpAvbpZsykdmc5mrwYCXYboyRopB2Ev1\n");

        // Expected success 
        let mut output = Vec::new();
        let res = GetTunnelCliCommand {
            code: "test".to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: 8vB4Pa1eGbQ2GpAvbpZsykdmc5mrwYCXYboyRopB2Ev1\r\ncode: test\r\nside_a: HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcb\r\nside_z: HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcf\r\ntunnel_type: MPLSoGRE\r\nbandwidth: 1000000000\r\nmtu: 1500\r\ndelay: 10000ms\r\njitter: 5000ms\r\ntunnel_net: 10.0.0.1/16\r\nstatus: activated\r\nowner: 8vB4Pa1eGbQ2GpAvbpZsykdmc5mrwYCXYboyRopB2Ev1\n");

    }
}
