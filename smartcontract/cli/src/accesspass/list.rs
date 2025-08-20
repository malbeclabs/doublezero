use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_program_common::serializer;
use doublezero_sdk::commands::accesspass::list::ListAccessPassCommand;
use doublezero_serviceability::state::accesspass::AccessPassStatus;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::{io::Write, net::Ipv4Addr};
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct ListAccessPassCliCommand {
    /// Output as pretty JSON
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

#[derive(Tabled, Serialize)]
pub struct AccessPassDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub account: Pubkey,
    pub accesspass_type: String,
    pub ip: Ipv4Addr,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub payer: Pubkey,
    pub last_access_epoch: u64,
    pub connections: u16,
    pub status: AccessPassStatus,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub owner: Pubkey,
}

impl ListAccessPassCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let access_passes = client.list_accesspass(ListAccessPassCommand)?;

        let mut access_pass_displays: Vec<AccessPassDisplay> = access_passes
            .into_iter()
            .map(|(pubkey, access_pass)| AccessPassDisplay {
                account: pubkey,
                accesspass_type: access_pass.accesspass_type.to_string(),
                ip: access_pass.client_ip,
                payer: access_pass.payer,
                last_access_epoch: access_pass.last_access_epoch,
                connections: access_pass.connection_count,
                status: access_pass.status,
                owner: access_pass.owner,
            })
            .collect();

        access_pass_displays.sort_by(|a, b| a.ip.cmp(&b.ip));

        let res = if self.json {
            serde_json::to_string_pretty(&access_pass_displays)?
        } else if self.json_compact {
            serde_json::to_string(&access_pass_displays)?
        } else {
            Table::new(access_pass_displays)
                .with(Style::psql().remove_horizontals())
                .to_string()
        };

        writeln!(out, "{res}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{accesspass::list::ListAccessPassCliCommand, tests::utils::create_test_client};
    use doublezero_sdk::AccountType;
    use doublezero_serviceability::state::accesspass::{
        AccessPass, AccessPassStatus, AccessPassType,
    };
    use solana_sdk::pubkey::Pubkey;
    use std::{collections::HashMap, net::Ipv4Addr};

    #[test]
    fn test_cli_accesspass_list() {
        let mut client = create_test_client();

        let access1_pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let access1 = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 2,
            client_ip: Ipv4Addr::new(1, 2, 3, 4),
            accesspass_type: AccessPassType::SolanaValidator,
            payer: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB"),
            last_access_epoch: 123,
            owner: Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB"),
            connection_count: 0,
            status: AccessPassStatus::Connected,
        };

        client.expect_list_accesspass().returning(move |_| {
            let mut access_passes = HashMap::new();
            access_passes.insert(access1_pubkey, access1.clone());
            Ok(access_passes)
        });

        let mut output = Vec::new();
        let res = ListAccessPassCliCommand {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, " account                                   | accesspass_type | ip      | payer                                     | last_access_epoch | connections | status    | owner                                     \n 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB | solanavalidator | 1.2.3.4 | 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB | 123               | 0           | connected | 1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB \n");

        let mut output = Vec::new();
        let res = ListAccessPassCliCommand {
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "[{\"account\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB\",\"accesspass_type\":\"solanavalidator\",\"ip\":\"1.2.3.4\",\"payer\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB\",\"last_access_epoch\":123,\"connections\":0,\"status\":\"Connected\",\"owner\":\"1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB\"}]\n");
    }
}
