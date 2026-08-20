use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_cli_core::{require, CliContext, RequirementCheck};
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

pub const LAMPORTS_PER_CREDIT: u64 = 1_000_000_000;

/// `doublezero balance [ADDRESS]`, mirroring `solana balance [ADDRESS]`: with no
/// argument it reports the configured keypair's balance, with one it reports the
/// balance of that account.
#[derive(Args, Debug, Default)]
pub struct BalanceCliCommand {
    /// Account to query (defaults to the configured keypair)
    pub pubkey: Option<Pubkey>,
}

impl BalanceCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        let balance = match self.pubkey {
            // Reading someone else's balance needs no local keypair, so the
            // keypair preflight only guards the own-balance form.
            Some(pubkey) => get_account_balance(client, pubkey)?,
            None => {
                require!(client, RequirementCheck::KEYPAIR);
                client.get_balance()?
            }
        };

        writeln!(out, "{} Credits", credits(balance))?;

        Ok(())
    }
}

/// Lamports held by `pubkey`, or `0` when the account does not exist. A missing
/// account is not an error here: an unfunded address has a balance of zero, and
/// `transfer` relies on that to detect a recipient it has to create.
pub fn get_account_balance<C: CliCommand>(client: &C, pubkey: Pubkey) -> eyre::Result<u64> {
    Ok(client
        .get_multiple_accounts(vec![pubkey])?
        .into_iter()
        .next()
        .flatten()
        .map(|account| account.lamports)
        .unwrap_or(0))
}

/// Lamports rendered in whole credits, the unit every user-facing amount uses.
pub fn credits(lamports: u64) -> f64 {
    lamports as f64 / LAMPORTS_PER_CREDIT as f64
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use mockall::predicate;
    use solana_sdk::account::Account;

    #[test]
    fn test_cli_balance_defaults_to_payer() {
        let mut client = create_test_client();
        client
            .expect_check_requirements()
            .returning(|_| Ok(()))
            .times(1);
        // create_test_client's payer balance is 10 lamports.

        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let res = block_on(BalanceCliCommand::default().execute(&ctx, &client, &mut out));

        assert!(res.is_ok());
        assert_eq!(
            String::from_utf8(out).unwrap(),
            "0.00000001 Credits\n".to_string()
        );
    }

    #[test]
    fn test_cli_balance_for_pubkey() {
        let pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let mut client = create_test_client();
        client
            .expect_get_multiple_accounts()
            .with(predicate::eq(vec![pubkey]))
            .returning(|_| {
                Ok(vec![Some(Account {
                    lamports: 2_500_000_000,
                    ..Account::default()
                })])
            });

        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let res = block_on(
            BalanceCliCommand {
                pubkey: Some(pubkey),
            }
            .execute(&ctx, &client, &mut out),
        );

        assert!(res.is_ok());
        assert_eq!(String::from_utf8(out).unwrap(), "2.5 Credits\n".to_string());
    }

    // An address that has never been funded has no account at all; report zero
    // rather than failing the lookup.
    #[test]
    fn test_cli_balance_for_missing_account_is_zero() {
        let pubkey = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let mut client = create_test_client();
        client
            .expect_get_multiple_accounts()
            .returning(|_| Ok(vec![None]));

        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let res = block_on(
            BalanceCliCommand {
                pubkey: Some(pubkey),
            }
            .execute(&ctx, &client, &mut out),
        );

        assert!(res.is_ok());
        assert_eq!(String::from_utf8(out).unwrap(), "0 Credits\n".to_string());
    }
}
