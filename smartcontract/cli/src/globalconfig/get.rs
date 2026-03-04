use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::globalconfig::get::GetGlobalConfigCommand;
use serde::Serialize;
use std::io::Write;
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct GetGlobalConfigCliCommand {
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
}

#[derive(Tabled, Serialize)]
pub struct ConfigDisplay {
    pub local_asn: u32,
    pub remote_asn: u32,
    pub device_tunnel_block: String,
    pub user_tunnel_block: String,
    pub multicast_group_block: String,
    pub multicast_publisher_block: String,
    pub next_bgp_community: u16,
}

impl GetGlobalConfigCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (_, config) = client.get_globalconfig(GetGlobalConfigCommand)?;

        let config_display = ConfigDisplay {
            local_asn: config.local_asn,
            remote_asn: config.remote_asn,
            device_tunnel_block: config.device_tunnel_block.to_string(),
            user_tunnel_block: config.user_tunnel_block.to_string(),
            multicast_group_block: config.multicastgroup_block.to_string(),
            multicast_publisher_block: config.multicast_publisher_block.to_string(),
            next_bgp_community: config.next_bgp_community,
        };

        if self.json {
            let json = serde_json::to_string_pretty(&config_display)?;
            writeln!(out, "{json}")?;
        } else {
            let headers = ConfigDisplay::headers();
            let fields = config_display.fields();
            let max_len = headers.iter().map(|h| h.len()).max().unwrap_or(0);
            for (header, value) in headers.iter().zip(fields.iter()) {
                writeln!(out, " {header:<max_len$} | {value}")?;
            }
        }

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

        // Table output
        let mut output = Vec::new();
        let res = GetGlobalConfigCliCommand { json: false }.execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        let has_row = |header: &str, value: &str| {
            output_str
                .lines()
                .any(|l| l.contains(header) && l.contains(value))
        };
        assert!(
            has_row("local_asn", "1234"),
            "local_asn row should contain value"
        );
        assert!(
            has_row("remote_asn", "5678"),
            "remote_asn row should contain value"
        );
        assert!(
            has_row("device_tunnel_block", "10.1.0.0/24"),
            "device_tunnel_block row should contain value"
        );

        // JSON output
        let mut output = Vec::new();
        let res = GetGlobalConfigCliCommand { json: true }.execute(&client, &mut output);
        assert!(res.is_ok());
        let json: serde_json::Value =
            serde_json::from_str(&String::from_utf8(output).unwrap()).unwrap();
        assert_eq!(json["local_asn"].as_u64().unwrap(), 1234);
        assert_eq!(json["remote_asn"].as_u64().unwrap(), 5678);
        assert_eq!(json["device_tunnel_block"].as_str().unwrap(), "10.1.0.0/24");
        assert_eq!(json["user_tunnel_block"].as_str().unwrap(), "10.5.0.0/24");
        assert_eq!(json["next_bgp_community"].as_u64().unwrap(), 10000);
    }
}
