use crate::doublezerocommand::CliCommand;
use crate::requirements::{CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::globalconfig::set::SetGlobalConfigCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct SetGlobalConfigCliCommand {
    #[arg(long)]
    pub local_asn: u32,
    #[arg(long)]
    pub remote_asn: u32,
    #[arg(long)]
    tunnel_tunnel_block: String,
    #[arg(long)]
    device_tunnel_block: String,
    #[arg(long)]
    multicastgroup_block: String,
}

impl SetGlobalConfigCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let signature = client.set_globalconfig(SetGlobalConfigCommand {
            local_asn: self.local_asn,
            remote_asn: self.remote_asn,
            tunnel_tunnel_block: networkv4_parse(&self.tunnel_tunnel_block),
            user_tunnel_block: networkv4_parse(&self.device_tunnel_block),
            multicastgroup_block: networkv4_parse(&self.multicastgroup_block),
        })?;
        writeln!(out, "Signature: {}", signature)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::globalconfig::set::SetGlobalConfigCliCommand;
    use crate::requirements::{CHECK_BALANCE, CHECK_ID_JSON};
    use crate::tests::tests::create_test_client;
    use doublezero_sdk::commands::globalconfig::set::SetGlobalConfigCommand;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_cli_globalconfig_set() {
        let mut client = create_test_client();

        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_set_globalconfig()
            .with(predicate::eq(SetGlobalConfigCommand {
                local_asn: 1234,
                remote_asn: 5678,
                tunnel_tunnel_block: ([10, 10, 0, 0], 16),
                user_tunnel_block: ([10, 20, 0, 0], 16),
                multicastgroup_block: ([224, 2, 0, 0], 4),
            }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = SetGlobalConfigCliCommand {
            local_asn: 1234,
            remote_asn: 5678,
            tunnel_tunnel_block: "10.10.0.0/16".to_string(),
            device_tunnel_block: "10.20.0.0/16".to_string(),
            multicastgroup_block: "224.2.0.0/4".to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
