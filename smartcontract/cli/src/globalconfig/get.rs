use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::globalconfig::get::GetGlobalConfigCommand;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct GetGlobalConfigCliCommand;

#[derive(Tabled)]
pub struct ConfigDisplay {
    #[tabled(rename = "local asn")]
    pub local_asn: u32,
    #[tabled(rename = "remote asn")]
    pub remote_asn: u32,
    #[tabled(rename = "link WAN block")]
    pub link_wan_block: String,
    #[tabled(rename = "link DZX block")]
    pub link_dzx_block: String,
    #[tabled(rename = "user tunnel block")]
    pub user_tunnel_block: String,
    #[tabled(rename = "multicast group block")]
    pub multicast_group_block: String,
}

impl GetGlobalConfigCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (_, config) = client.get_globalconfig(GetGlobalConfigCommand)?;

        let config_display = ConfigDisplay {
            local_asn: config.local_asn,
            remote_asn: config.remote_asn,
            link_wan_block: config.link_wan_block.to_string(),
            link_dzx_block: config.link_dzx_block.to_string(),
            user_tunnel_block: config.user_tunnel_block.to_string(),
            multicast_group_block: config.multicastgroup_block.to_string(),
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
            link_wan_block: "10.1.0.0/24".parse().unwrap(),
            link_dzx_block: "10.2.0.0/24".parse().unwrap(),
            user_tunnel_block: "10.5.0.0/24".parse().unwrap(),
            multicastgroup_block: "224.2.0.0/4".parse().unwrap(),
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
            output_str, " local asn | remote asn | link WAN block | link DZX block | user tunnel block | multicast group block \n 1234      | 5678       | 10.1.0.0/24    | 10.2.0.0/24    | 10.5.0.0/24       | 224.2.0.0/4           \n"
        );
    }
}
