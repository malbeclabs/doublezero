use crate::{
    doublezerocommand::CliCommand,
    pool_for_activation::poll_for_multicastgroup_activated,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::{validate_code, validate_pubkey_or_code},
};
use clap::Args;
use doublezero_sdk::commands::multicastgroup::{
    get::GetMulticastGroupCommand, update::UpdateMulticastGroupCommand,
};
use std::{io::Write, net::Ipv4Addr};

#[derive(Args, Debug)]
pub struct UpdateMulticastGroupCliCommand {
    /// Multicast group Pubkey to update
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
    /// Updated code for the multicast group
    #[arg(long, value_parser = validate_code)]
    pub code: Option<String>,
    /// Updated multicast IPv4 address (e.g. 239.0.0.1)
    #[arg(long)]
    pub multicast_ip: Option<Ipv4Addr>,
    /// Updated maximum bandwidth (e.g. 1Gbps, 100Mbps)
    #[arg(long)]
    pub max_bandwidth: Option<u64>,
    /// Wait for the multicast group to be activated
    #[arg(short, long, default_value_t = false)]
    pub wait: bool,
}

impl UpdateMulticastGroupCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (pubkey, _) = client.get_multicastgroup(GetMulticastGroupCommand {
            pubkey_or_code: self.pubkey,
        })?;

        let signature = client.update_multicastgroup(UpdateMulticastGroupCommand {
            pubkey,
            code: self.code.clone(),
            multicast_ip: self.multicast_ip,
            max_bandwidth: self.max_bandwidth,
        })?;
        writeln!(out, "Signature: {signature}",)?;

        if self.wait {
            let user = poll_for_multicastgroup_activated(client, &pubkey)?;
            writeln!(out, "Status: {0}", user.status)?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand,
        multicastgroup::update::UpdateMulticastGroupCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::multicastgroup::{
            get::GetMulticastGroupCommand, update::UpdateMulticastGroupCommand,
        },
        get_multicastgroup_pda, AccountType, MulticastGroup, MulticastGroupStatus,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_multicastgroup_update() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_multicastgroup_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let multicastgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            tenant_pk: Pubkey::new_unique(),
            multicast_ip: [10, 0, 0, 1].into(),
            max_bandwidth: 1000000000,
            pub_allowlist: vec![],
            sub_allowlist: vec![],
            publishers: vec![],
            subscribers: vec![],
            status: MulticastGroupStatus::Activated,
            owner: pda_pubkey,
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: pda_pubkey.to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, multicastgroup.clone())));
        client
            .expect_update_multicastgroup()
            .with(predicate::eq(UpdateMulticastGroupCommand {
                pubkey: pda_pubkey,
                code: Some("new_code".to_string()),
                multicast_ip: Some([10, 0, 0, 1].into()),
                max_bandwidth: Some(1000000000),
            }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = UpdateMulticastGroupCliCommand {
            pubkey: pda_pubkey.to_string(),
            code: Some("new_code".to_string()),
            multicast_ip: Some([10, 0, 0, 1].into()),
            max_bandwidth: Some(1000000000),
            wait: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
