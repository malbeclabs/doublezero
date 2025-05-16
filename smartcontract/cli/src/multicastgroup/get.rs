use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::multicastgroup::get::GetMulticastGroupCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetMulticastGroupCliCommand {
    #[arg(long)]
    pub code: String,
}

impl GetMulticastGroupCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, multicastgroup) = client.get_multicastgroup(GetMulticastGroupCommand {
            pubkey_or_code: self.code,
        })?;

        writeln!(out,
        "account: {}\r\ncode: {}\r\nmulticast_ip: {}\r\nmax_bandwidth: {}\r\nstatus: {}\r\nowner: {}",
        pubkey,
        multicastgroup.code,
        ipv4_to_string(&multicastgroup.multicast_ip),
        bandwidth_to_string(multicastgroup.max_bandwidth),
        multicastgroup.status,
        multicastgroup.owner
        )?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::doublezerocommand::CliCommand;
    use crate::multicastgroup::get::GetMulticastGroupCliCommand;
    use crate::tests::tests::create_test_client;
    use doublezero_sdk::commands::multicastgroup::get::GetMulticastGroupCommand;
    use doublezero_sdk::{
        get_multicastgroup_pda, AccountType, MulticastGroup, MulticastGroupStatus,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_multicastgroup_get() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_multicastgroup_pda(&client.get_program_id(), 1);

        let multicastgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            tenant_pk: Pubkey::new_unique(),
            multicast_ip: [10, 0, 0, 1],
            max_bandwidth: 1000000000,
            pub_allowlist: vec![],
            sub_allowlist: vec![],
            publishers: vec![],
            subscribers: vec![],
            status: MulticastGroupStatus::Activated,
            owner: pda_pubkey,
        };

        let multicastgroup2 = multicastgroup.clone();
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: pda_pubkey.to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, multicastgroup.clone())));
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: "test".to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, multicastgroup2.clone())));
        client
            .expect_get_multicastgroup()
            .returning(move |_| Err(eyre::eyre!("not found")));
        /*****************************************************************************************************/
        // Expected failure
        let mut output = Vec::new();
        let res = GetMulticastGroupCliCommand {
            code: Pubkey::new_unique().to_string(),
        }
        .execute(&client, &mut output);
        assert!(!res.is_ok(), "I shouldn't find anything.");

        // Expected success
        let mut output = Vec::new();
        let res = GetMulticastGroupCliCommand {
            code: pda_pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by pubkey");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: CahibSNzuzo1MZhonHXLw3bJRmZoecDSJRXWdH4WFYJK\r\ncode: test\r\nmulticast_ip: 10.0.0.1\r\nmax_bandwidth: 1Gbps\r\nstatus: activated\r\nowner: CahibSNzuzo1MZhonHXLw3bJRmZoecDSJRXWdH4WFYJK\n");

        // Expected success
        let mut output = Vec::new();
        let res = GetMulticastGroupCliCommand {
            code: "test".to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "I should find a item by code");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: CahibSNzuzo1MZhonHXLw3bJRmZoecDSJRXWdH4WFYJK\r\ncode: test\r\nmulticast_ip: 10.0.0.1\r\nmax_bandwidth: 1Gbps\r\nstatus: activated\r\nowner: CahibSNzuzo1MZhonHXLw3bJRmZoecDSJRXWdH4WFYJK\n");
    }
}
