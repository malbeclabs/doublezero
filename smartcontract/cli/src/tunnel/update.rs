use crate::doublezerocommand::CliCommand;
use crate::requirements::{CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::tunnel::get::GetTunnelCommand;
use doublezero_sdk::commands::tunnel::update::UpdateTunnelCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct UpdateTunnelCliCommand {
    #[arg(long)]
    pub pubkey: String,
    #[arg(long)]
    pub code: Option<String>,
    #[arg(long)]
    pub tunnel_type: Option<String>,
    #[arg(long)]
    pub bandwidth: Option<String>,
    #[arg(long)]
    pub mtu: Option<u32>,
    #[arg(long)]
    pub delay_ms: Option<f64>,
    #[arg(long)]
    pub jitter_ms: Option<f64>,
}

impl UpdateTunnelCliCommand {
    pub fn execute<W: Write>(self, client: &dyn CliCommand, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (_, tunnel) = client.get_tunnel(GetTunnelCommand {
            pubkey_or_code: self.pubkey,
        })?;

        let signature = client.update_tunnel(UpdateTunnelCommand {
            index: tunnel.index,
            code: self.code.clone(),
            tunnel_type: self.tunnel_type.map(|t| t.parse().unwrap()),
            bandwidth: self.bandwidth.map(|b| bandwidth_parse(&b)),
            mtu: self.mtu,
            delay_ns: self.delay_ms.map(|delay_ms| (delay_ms * 1000000.0) as u64),
            jitter_ns: self
                .jitter_ms
                .map(|jitter_ms| (jitter_ms * 1000000.0) as u64),
        })?;
        writeln!(out, "Signature: {}", signature)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::doublezerocommand::CliCommand;
    use crate::requirements::{CHECK_BALANCE, CHECK_ID_JSON};
    use crate::tests::tests::create_test_client;
    use crate::tunnel::update::UpdateTunnelCliCommand;
    use doublezero_sdk::commands::tunnel::get::GetTunnelCommand;
    use doublezero_sdk::commands::tunnel::update::UpdateTunnelCommand;
    use doublezero_sdk::get_tunnel_pda;
    use doublezero_sdk::AccountType;
    use doublezero_sdk::Tunnel;
    use doublezero_sdk::TunnelStatus;
    use doublezero_sdk::TunnelTunnelType;
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_cli_tunnel_update() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_tunnel_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

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

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_tunnel()
            .with(predicate::eq(GetTunnelCommand {
                pubkey_or_code: pda_pubkey.to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, tunnel.clone())));
        client
            .expect_update_tunnel()
            .with(predicate::eq(UpdateTunnelCommand {
                index: 1,
                code: Some("new_code".to_string()),
                tunnel_type: None,
                bandwidth: Some(1000000000000),
                mtu: Some(1500),
                delay_ns: Some(10000000),
                jitter_ns: Some(5000000),
            }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = UpdateTunnelCliCommand {
            pubkey: pda_pubkey.to_string(),
            code: Some("new_code".to_string()),
            tunnel_type: None,
            bandwidth: Some("1000000000".to_string()),
            mtu: Some(1500),
            delay_ms: Some(10.0),
            jitter_ms: Some(5.0),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
