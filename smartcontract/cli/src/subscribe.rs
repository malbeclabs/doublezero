use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_cli_core::CliContext;
use std::io::Write;

#[derive(Args, Debug)]
pub struct SubscribeCliCommand;

impl SubscribeCliCommand {
    /// Subscribe to all program account state.
    ///
    /// The blocking websocket subscription (`DZClient::subscribe`) is not
    /// representable through the generic `CliCommand` trait because mockall
    /// does not support `dyn FnMut` parameters. This verb therefore uses
    /// `get_all()` — a single snapshot of all accounts — through the trait.
    /// The binary may override dispatch to call the blocking subscribe loop
    /// directly via `CliCommandImpl` when a persistent stream is needed.
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        tracing::debug!(env = %ctx.env, "subscribe");

        writeln!(out, "Fetching current accounts...")?;

        let accounts = client.get_all()?;
        for (pubkey, account) in &accounts {
            writeln!(out, "{pubkey} -> {account:?}")?;
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
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;

    #[test]
    fn test_subscribe_writes_to_out() {
        let mut client = create_test_client();

        let pk = Pubkey::new_unique();
        let mut accounts: HashMap<Box<Pubkey>, Box<AccountData>> = HashMap::new();
        accounts.insert(Box::new(pk), Box::new(AccountData::None));
        client
            .expect_get_all()
            .returning(move || Ok(accounts.clone()));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(SubscribeCliCommand.execute(&ctx, &client, &mut output));
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("Fetching current accounts..."));
        assert!(output_str.contains(&pk.to_string()));
    }
}
