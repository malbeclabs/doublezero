use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_program_common::types::NetworkV4;
use doublezero_sdk::{
    commands::globalconfig::set::SetGlobalConfigCommand, BGP_COMMUNITY_MAX, BGP_COMMUNITY_MIN,
};
use std::io::Write;

#[derive(Args, Debug)]
pub struct SetGlobalConfigCliCommand {
    /// Local ASN (Autonomous System Number)
    #[arg(long)]
    pub local_asn: Option<u32>,
    /// Remote ASN (Autonomous System Number)
    #[arg(long)]
    pub remote_asn: Option<u32>,
    /// Link tunnel block in CIDR format
    #[arg(long)]
    device_tunnel_block: Option<NetworkV4>,
    /// Device tunnel block in CIDR format
    #[arg(long)]
    user_tunnel_block: Option<NetworkV4>,
    /// Multicast group block in CIDR format
    #[arg(long)]
    multicastgroup_block: Option<NetworkV4>,
    /// Next BGP community value to assign
    #[arg(long)]
    pub next_bgp_community: Option<u16>,
    /// Multicast publisher block in CIDR format
    #[arg(long)]
    multicast_publisher_block: Option<NetworkV4>,
}

impl SetGlobalConfigCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        if let Some(bgp_community) = self.next_bgp_community {
            if !(BGP_COMMUNITY_MIN..=BGP_COMMUNITY_MAX).contains(&bgp_community) {
                return Err(eyre::eyre!(
                    "BGP community {} is out of valid range {}-{}",
                    bgp_community,
                    BGP_COMMUNITY_MIN,
                    BGP_COMMUNITY_MAX
                ));
            }
        }

        let signature = client.set_globalconfig(SetGlobalConfigCommand {
            local_asn: self.local_asn,
            remote_asn: self.remote_asn,
            device_tunnel_block: self.device_tunnel_block,
            user_tunnel_block: self.user_tunnel_block,
            multicastgroup_block: self.multicastgroup_block,
            next_bgp_community: self.next_bgp_community,
            multicast_publisher_block: self.multicast_publisher_block,
        })?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        globalconfig::set::SetGlobalConfigCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
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
                local_asn: Some(1234),
                remote_asn: Some(5678),
                device_tunnel_block: "10.20.0.0/16".parse().ok(),
                user_tunnel_block: "10.10.0.0/16".parse().ok(),
                multicastgroup_block: "224.2.0.0/4".parse().ok(),
                multicast_publisher_block: None,
                next_bgp_community: None,
            }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        // Set all global config; reflects initializing global config or updating all config values
        let mut output1 = Vec::new();
        let res = SetGlobalConfigCliCommand {
            local_asn: Some(1234),
            remote_asn: Some(5678),
            device_tunnel_block: "10.20.0.0/16".parse().ok(),
            user_tunnel_block: "10.10.0.0/16".parse().ok(),
            multicastgroup_block: "224.2.0.0/4".parse().ok(),
            multicast_publisher_block: None,
            next_bgp_community: None,
        }
        .execute(&client, &mut output1);
        assert!(res.is_ok());
        let output_str1 = String::from_utf8(output1).unwrap();
        assert_eq!(
            output_str1,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );

        // Set partial global config; updating select config values
        client
            .expect_set_globalconfig()
            .with(predicate::eq(SetGlobalConfigCommand {
                local_asn: Some(9876),
                remote_asn: Some(5432),
                device_tunnel_block: None,
                user_tunnel_block: None,
                multicastgroup_block: None,
                multicast_publisher_block: None,
                next_bgp_community: None,
            }))
            .returning(move |_| Ok(signature));
        let mut output2 = Vec::new();
        let res = SetGlobalConfigCliCommand {
            local_asn: Some(9876),
            remote_asn: Some(5432),
            device_tunnel_block: None,
            user_tunnel_block: None,
            multicastgroup_block: None,
            multicast_publisher_block: None,
            next_bgp_community: None,
        }
        .execute(&client, &mut output2);
        assert!(res.is_ok());
        let output_str2 = String::from_utf8(output2).unwrap();
        assert_eq!(
            output_str2,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }

    #[test]
    fn test_cli_globalconfig_set_empty() {
        let mut client = create_test_client();

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_set_globalconfig()
            .with(predicate::eq(SetGlobalConfigCommand {
                local_asn: None,
                remote_asn: None,
                device_tunnel_block: None,
                user_tunnel_block: None,
                multicastgroup_block: None,
                multicast_publisher_block: None,
                next_bgp_community: None,
            }))
            .returning(move |_| {
                Err(eyre::eyre!(
                    "Invalid SetGlobalConfigCommand; no updates specified"
                ))
            });

        let mut output = vec![];
        let res = SetGlobalConfigCliCommand {
            local_asn: None,
            remote_asn: None,
            device_tunnel_block: None,
            user_tunnel_block: None,
            multicastgroup_block: None,
            multicast_publisher_block: None,
            next_bgp_community: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
    }
}
