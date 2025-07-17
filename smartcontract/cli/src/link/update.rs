use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::{
        validate_code, validate_parse_bandwidth, validate_parse_delay_ms, validate_parse_jitter_ms,
        validate_parse_mtu, validate_pubkey,
    },
};
use clap::Args;
use doublezero_sdk::commands::link::{get::GetLinkCommand, update::UpdateLinkCommand};
use eyre::eyre;
use std::io::Write;

#[derive(Args, Debug)]
pub struct UpdateLinkCliCommand {
    /// Link Pubkey or code to update
    #[arg(long, value_parser = validate_pubkey)]
    pub pubkey: String,
    /// Updated code for the link
    #[arg(long, value_parser = validate_code)]
    pub code: Option<String>,
    /// Updated tunnel type (e.g. L1, L2, L3)
    #[arg(long)]
    pub tunnel_type: Option<String>,
    /// Updated bandwidth (e.g. 1Gbps, 100Mbps)
    #[arg(long, value_parser = validate_parse_bandwidth)]
    pub bandwidth: Option<u64>,
    /// Updated MTU (Maximum Transmission Unit) in bytes
    #[arg(long, value_parser = validate_parse_mtu)]
    pub mtu: Option<u32>,
    /// RTT (Round Trip Time) delay in milliseconds
    #[arg(long, value_parser = validate_parse_delay_ms)]
    pub delay_ms: Option<f64>,
    /// Jitter in milliseconds
    #[arg(long, value_parser = validate_parse_jitter_ms)]
    pub jitter_ms: Option<f64>,
}

impl UpdateLinkCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (pubkey, _) = client.get_link(GetLinkCommand {
            pubkey_or_code: self.pubkey,
        })?;

        let tunnel_type = self
            .tunnel_type
            .map(|t| t.parse())
            .transpose()
            .map_err(|e| eyre!("Invalid tunnel type: {e}"))?;

        let signature = client.update_link(UpdateLinkCommand {
            pubkey,
            code: self.code.clone(),
            tunnel_type,
            bandwidth: self.bandwidth,
            mtu: self.mtu,
            delay_ns: self.delay_ms.map(|delay_ms| (delay_ms * 1000000.0) as u64),
            jitter_ns: self
                .jitter_ms
                .map(|jitter_ms| (jitter_ms * 1000000.0) as u64),
        })?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand,
        link::update::UpdateLinkCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::link::{get::GetLinkCommand, update::UpdateLinkCommand},
        get_link_pda, AccountType, Link, LinkLinkType, LinkStatus,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_link_update() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_link_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
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
            link_type: LinkLinkType::L3,
            bandwidth: 1000000000,
            mtu: 1500,
            delay_ns: 10000000000,
            jitter_ns: 5000000000,
            tunnel_id: 1,
            tunnel_net: "10.0.0.1/16".parse().unwrap(),
            status: LinkStatus::Activated,
            owner: pda_pubkey,
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_link()
            .with(predicate::eq(GetLinkCommand {
                pubkey_or_code: pda_pubkey.to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, tunnel.clone())));
        client
            .expect_update_link()
            .with(predicate::eq(UpdateLinkCommand {
                pubkey: pda_pubkey,
                code: Some("new_code".to_string()),
                tunnel_type: None,
                bandwidth: Some(1000000000),
                mtu: Some(1500),
                delay_ns: Some(10000000),
                jitter_ns: Some(5000000),
            }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = UpdateLinkCliCommand {
            pubkey: pda_pubkey.to_string(),
            code: Some("new_code".to_string()),
            tunnel_type: None,
            bandwidth: Some(1000000000),
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
