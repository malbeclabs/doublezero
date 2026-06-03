use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_serviceability::state::accountdata::AccountData;
use serde_json::to_writer_pretty;
use std::io::Write;

/// Dump all program accounts.
///
/// This is a diagnostic verb that returns every account owned by the
/// serviceability program. The plural `Accounts` name is intentional — it
/// is a genuine "list all accounts" dump, not a single-resource verb.
#[derive(Args, Debug)]
pub struct GetAccountsCliCommand {
    /// Filter by account type
    #[arg(long)]
    pub account_type: Option<String>,

    /// Suppress output
    #[arg(long, default_value_t = false)]
    pub no_output: bool,
}

#[derive(serde::Serialize, serde::Deserialize)]
pub struct GetAccountsCliResponse {
    pub env: String,
    pub accounts: Vec<AccountsCliResponse>,
}

#[derive(serde::Serialize, serde::Deserialize)]
pub struct AccountsCliResponse {
    pub pubkey: String,
    pub account_type: String,
    pub account: Box<AccountData>,
}

impl GetAccountsCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        tracing::debug!(env = %ctx.env, "accounts");

        let mut accounts: Vec<(String, Box<AccountData>)> = client
            .get_all()?
            .into_iter()
            .map(|acc| (acc.0.to_string(), acc.1))
            .collect::<Vec<_>>();

        if let Some(ref account_type) = self.account_type {
            let account_type = account_type.to_lowercase();
            accounts.retain(|(_, acc)| acc.get_name().to_lowercase() == account_type);
        }

        let res = GetAccountsCliResponse {
            env: ctx.env.to_string(),
            accounts: accounts
                .into_iter()
                .map(|(pubkey, account)| AccountsCliResponse {
                    pubkey,
                    account_type: account.get_name().to_string(),
                    account,
                })
                .collect(),
        };

        if !self.no_output {
            to_writer_pretty(&mut *out, &res)?;
            writeln!(out)?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use std::collections::HashMap;

    #[test]
    fn test_accounts_uses_ctx_env() {
        let mut client = create_test_client();
        client.expect_get_all().returning(|| Ok(HashMap::new()));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetAccountsCliCommand {
                account_type: None,
                no_output: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains(&ctx.env.to_string()));
    }

    #[test]
    fn test_accounts_no_output() {
        let mut client = create_test_client();
        client.expect_get_all().returning(|| Ok(HashMap::new()));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            GetAccountsCliCommand {
                account_type: None,
                no_output: true,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        assert!(output.is_empty());
    }
}
