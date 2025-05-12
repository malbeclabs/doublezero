use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::globalconfig::get::GetGlobalConfigCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetGlobalConfigCliCommand {}

impl GetGlobalConfigCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (_, config) = client.get_globalconfig(GetGlobalConfigCommand {})?;

        writeln!(
            out,
            "local-asn: {}\r\nremote-asn: {}\r\ndevice_tunnel_block: {}\r\nuser_tunnel_block: {}",
            config.local_asn,
            config.remote_asn,
            networkv4_to_string(&config.tunnel_tunnel_block),
            networkv4_to_string(&config.user_tunnel_block),
        )?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::doublezerocommand::CliCommand;
    use crate::globalconfig::get::GetGlobalConfigCliCommand;
    use crate::tests::tests::create_test_client;
    use doublezero_sdk::{GetGlobalConfigCommand, GlobalConfig};
    use doublezero_sla_program::pda::get_globalconfig_pda;
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
            tunnel_tunnel_block: ([10, 1, 0, 0], 24),
            user_tunnel_block: ([10, 5, 0, 0], 24),
        };

        client
            .expect_get_globalconfig()
            .with(predicate::eq(GetGlobalConfigCommand {}))
            .returning(move |_| Ok((pubkey, globalconfig.clone())));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = GetGlobalConfigCliCommand {}.execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"local-asn: 1234\r\nremote-asn: 5678\r\ndevice_tunnel_block: 10.1.0.0/24\r\nuser_tunnel_block: 10.5.0.0/24\n"
        );
    }
}
