use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_program_common::serializer;
use doublezero_sdk::GetGlobalStateCommand;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct GetAuthorityCliCommand;

#[derive(Tabled, Serialize)]
pub struct AuthorityDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub activator_authority: Pubkey,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub access_authority: Pubkey,
}

impl GetAuthorityCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (_, gstate) = client.get_globalstate(GetGlobalStateCommand)?;

        let config_display = AuthorityDisplay {
            activator_authority: gstate.activator_authority_pk,
            access_authority: gstate.sentinel_authority_pk,
        };
        let config_displays = vec![config_display];
        let table = Table::new(config_displays)
            .with(Style::psql().remove_horizontals())
            .to_string();
        writeln!(out, "{table}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand, globalconfig::get::GetGlobalConfigCliCommand,
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{GetGlobalConfigCommand, GlobalConfig};
    use doublezero_serviceability::pda::get_globalconfig_pda;
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_globalconfig_get() {
        let mut client = create_test_client();

        let (pubkey, bump_seed) = get_globalconfig_pda(&client.get_program_id());
        let globalconfig = GlobalConfig {
            account_type: doublezero_sdk::AccountType::GlobalState,
            owner: Pubkey::from_str_const("11111112D1oxKts8YPdTJRG5FzxTNpMtWmq8hkVx3"),
            bump_seed,
            local_asn: 1234,
            remote_asn: 5678,
            device_tunnel_block: "10.1.0.0/24".parse().unwrap(),
            user_tunnel_block: "10.5.0.0/24".parse().unwrap(),
            multicastgroup_block: "224.2.0.0/4".parse().unwrap(),
            multicast_publisher_block: "148.51.120.0/21".parse().unwrap(),
            next_bgp_community: 10000,
        };

        client
            .expect_get_globalconfig()
            .with(predicate::eq(GetGlobalConfigCommand))
            .returning(move |_| Ok((pubkey, globalconfig.clone())));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = GetGlobalConfigCliCommand.execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str, " local asn | remote asn | device tunnel block | user tunnel block | multicast group block | multicast publisher block | next bgp community \n 1234      | 5678       | 10.1.0.0/24         | 10.5.0.0/24       | 224.2.0.0/4           | 148.51.120.0/21           | 10000              \n"
        );
    }
}
