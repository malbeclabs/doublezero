use clap::Args;
use doublezero_cli_core::CliContext;
use serde::Serialize;
use std::io::Write;

use crate::{doublezerocommand::CliCommand, validators::validate_pubkey_or_code};

#[derive(Args, Debug)]
pub struct GetAccountCliCommand {
    /// Public key of the account to retrieve
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
    /// Include transaction logs in the output
    #[arg(long, action = clap::ArgAction::SetTrue)]
    pub logs: bool,
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
}

#[derive(Serialize)]
struct AccountJsonOutput {
    pub name: String,
    pub args: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub transactions: Option<Vec<TransactionDisplay>>,
}

#[derive(Serialize)]
struct TransactionDisplay {
    pub time: String,
    pub instruction: String,
    pub instruction_args: String,
    pub pubkey: String,
    pub signature: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub log_messages: Option<Vec<String>>,
}

impl GetAccountCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        tracing::debug!(env = %ctx.env, pubkey = %self.pubkey, "account get");

        let pubkey: solana_sdk::pubkey::Pubkey = self
            .pubkey
            .parse()
            .map_err(|_| eyre::eyre!("Invalid pubkey"))?;

        let account = client.get_account_data(pubkey)?;
        let transactions = client.get_transactions(pubkey).ok();

        if self.json {
            let json_out = AccountJsonOutput {
                name: account.get_name().to_string(),
                args: account.get_args().to_string(),
                transactions: transactions.map(|trans| {
                    trans
                        .into_iter()
                        .map(|t| TransactionDisplay {
                            time: t.time.to_string(),
                            instruction: t.instruction.get_name().to_string(),
                            instruction_args: t.instruction.get_args().to_string(),
                            pubkey: t.account.to_string(),
                            signature: t.signature.to_string(),
                            log_messages: if self.logs {
                                Some(t.log_messages)
                            } else {
                                None
                            },
                        })
                        .collect()
                }),
            };
            writeln!(out, "{}", serde_json::to_string_pretty(&json_out)?)?;
        } else {
            writeln!(out, "{} ({})", account.get_name(), account.get_args())?;
            writeln!(out)?;

            if let Some(trans) = transactions {
                writeln!(out, "Transactions:")?;
                for tran in trans {
                    writeln!(
                        out,
                        "{}: {} ({})\n\t\t\tpubkey: {}, signature: {}",
                        &tran.time.to_string(),
                        tran.instruction.get_name(),
                        tran.instruction.get_args(),
                        tran.account,
                        tran.signature
                    )?;

                    if self.logs {
                        for msg in tran.log_messages {
                            writeln!(out, "  - {msg}")?;
                        }
                        writeln!(out)?;
                    }
                }
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_serviceability::state::accountdata::AccountData;
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    fn make_test_account() -> AccountData {
        AccountData::None
    }

    #[test]
    fn test_account_get_table_output() {
        let mut client = create_test_client();
        let pk = Pubkey::new_unique();

        client
            .expect_get_account_data()
            .with(predicate::eq(pk))
            .returning(|_| Ok(make_test_account()));
        client
            .expect_get_transactions()
            .with(predicate::eq(pk))
            .returning(|_| Ok(vec![]));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetAccountCliCommand {
                pubkey: pk.to_string(),
                logs: false,
                json: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("None"));
    }

    #[test]
    fn test_account_get_json_output() {
        let mut client = create_test_client();
        let pk = Pubkey::new_unique();

        client
            .expect_get_account_data()
            .with(predicate::eq(pk))
            .returning(|_| Ok(make_test_account()));
        client
            .expect_get_transactions()
            .with(predicate::eq(pk))
            .returning(|_| Ok(vec![]));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetAccountCliCommand {
                pubkey: pk.to_string(),
                logs: false,
                json: true,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let json: serde_json::Value =
            serde_json::from_str(&String::from_utf8(output).unwrap()).unwrap();
        assert!(json["name"].is_string());
    }
}
