use clap::Args;
use doublezero_cli_core::CliContext;
use std::io::Write;

use crate::{doublezerocommand::CliCommand, validators::validate_pubkey};

#[derive(Args, Debug)]
pub struct LogCliCommand {
    /// Public key of the user to get logs for
    #[arg(long, value_parser = validate_pubkey)]
    pub pubkey: String,
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
}

impl LogCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        tracing::debug!(env = %ctx.env, pubkey = %self.pubkey, "log");

        let pubkey: solana_sdk::pubkey::Pubkey = self
            .pubkey
            .parse()
            .map_err(|_| eyre::eyre!("Invalid pubkey"))?;

        let logs = client.get_logs(&pubkey)?;

        if self.json {
            serde_json::to_writer_pretty(&mut *out, &logs)?;
            writeln!(out)?;
        } else {
            for msg in &logs {
                writeln!(out, "{msg}")?;
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
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_log_table_output() {
        let mut client = create_test_client();
        let pk = Pubkey::new_unique();

        client
            .expect_get_logs()
            .with(predicate::eq(pk))
            .returning(|_| Ok(vec!["log line 1".to_string(), "log line 2".to_string()]));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            LogCliCommand {
                pubkey: pk.to_string(),
                json: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("log line 1"));
        assert!(output_str.contains("log line 2"));
    }

    #[test]
    fn test_log_json_output() {
        let mut client = create_test_client();
        let pk = Pubkey::new_unique();

        client
            .expect_get_logs()
            .with(predicate::eq(pk))
            .returning(|_| Ok(vec!["log line 1".to_string()]));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            LogCliCommand {
                pubkey: pk.to_string(),
                json: true,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok());
        let json: serde_json::Value =
            serde_json::from_str(&String::from_utf8(output).unwrap()).unwrap();
        assert!(json.is_array());
        assert_eq!(json[0].as_str().unwrap(), "log line 1");
    }
}
