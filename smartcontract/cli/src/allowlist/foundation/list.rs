use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::allowlist::foundation::list::ListFoundationAllowlistCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct ListFoundationAllowlistCliCommand {
    /// Output as pretty JSON
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

impl ListFoundationAllowlistCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let list = client.list_foundation_allowlist(ListFoundationAllowlistCommand)?;

        if self.json || self.json_compact {
            let list = list
                .into_iter()
                .map(|pubkey| pubkey.to_string())
                .collect::<Vec<_>>();

            let json = {
                if self.json_compact {
                    serde_json::to_string(&list)?
                } else {
                    serde_json::to_string_pretty(&list)?
                }
            };
            writeln!(out, "{json}")?;
        } else {
            writeln!(out, "Pubkeys:")?;
            for user in list {
                writeln!(out, "\t{user}")?;
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        allowlist::foundation::list::ListFoundationAllowlistCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::commands::allowlist::foundation::list::ListFoundationAllowlistCommand;
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_foundation_allowlist_list() {
        let mut client = create_test_client();

        let pubkey1 = Pubkey::from_str_const("1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM");
        let pubkey2 = Pubkey::from_str_const("1111111ogCyDbaRMvkdsHB3qfdyFYaG1WtRUAfdh");
        let pubkey3 = Pubkey::from_str_const("11111112D1oxKts8YPdTJRG5FzxTNpMtWmq8hkVx3");

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_list_foundation_allowlist()
            .with(predicate::eq(ListFoundationAllowlistCommand))
            .returning(move |_| Ok(vec![pubkey1, pubkey2, pubkey3]));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = ListFoundationAllowlistCliCommand {
            json: false,
            json_compact: false,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Pubkeys:\n\t1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM\n\t1111111ogCyDbaRMvkdsHB3qfdyFYaG1WtRUAfdh\n\t11111112D1oxKts8YPdTJRG5FzxTNpMtWmq8hkVx3\n"
        );

        let mut output = Vec::new();
        let res = ListFoundationAllowlistCliCommand {
            json: false,
            json_compact: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"[\"1111111QLbz7JHiBTspS962RLKV8GndWFwiEaqKM\",\"1111111ogCyDbaRMvkdsHB3qfdyFYaG1WtRUAfdh\",\"11111112D1oxKts8YPdTJRG5FzxTNpMtWmq8hkVx3\"]\n"
        );
    }
}
