use crate::{
    balance::{credits, get_account_balance, LAMPORTS_PER_CREDIT},
    doublezerocommand::CliCommand,
};
use clap::Args;
use doublezero_cli_core::{require, CliContext, RequirementCheck};
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

/// Fee for the single-signature transfer transaction, reserved out of the
/// sender's balance so an `ALL` transfer stays payable.
const SIGNATURE_FEE_LAMPORTS: u64 = 5_000;

/// A wallet account holds no data, so rent exemption for `0` bytes is the floor
/// a brand-new recipient has to land on.
const WALLET_DATA_LEN: usize = 0;

/// `doublezero transfer <RECIPIENT> <AMOUNT>`, mirroring `solana transfer`:
/// AMOUNT is given in credits, or `ALL` to send the whole balance minus the
/// transaction fee.
///
/// Unlike `solana transfer`, an unfunded recipient does not need an opt-in flag:
/// the system transfer creates the account, and the amount is topped up to the
/// rent-exempt minimum when it would otherwise be too small for the runtime to
/// accept.
///
/// The same minimum applies to the sender: the runtime rejects a transaction
/// that leaves an account holding a nonzero balance below it, so a transfer is
/// refused unless the sender ends up either empty or at or above that minimum.
#[derive(Args, Debug)]
pub struct TransferCliCommand {
    /// Recipient account
    pub to: Pubkey,
    /// Amount to send in credits, or `ALL` to send the entire balance
    #[arg(value_parser = parse_transfer_amount)]
    pub amount: TransferAmount,
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub enum TransferAmount {
    /// Everything the sender holds, minus the transaction fee.
    All,
    Lamports(u64),
}

/// Parse an `AMOUNT` argument: `ALL` (any casing) or a credit amount.
fn parse_transfer_amount(val: &str) -> Result<TransferAmount, String> {
    if val.eq_ignore_ascii_case("ALL") {
        return Ok(TransferAmount::All);
    }

    let credits: f64 = val
        .parse()
        .map_err(|_| format!("invalid amount '{val}': expected a credit amount or 'ALL'"))?;
    if !credits.is_finite() || credits < 0.0 {
        return Err(format!("invalid amount '{val}': must be zero or positive"));
    }

    let lamports = (credits * LAMPORTS_PER_CREDIT as f64).round();
    if lamports > u64::MAX as f64 {
        return Err(format!("invalid amount '{val}': too large"));
    }

    Ok(TransferAmount::Lamports(lamports as u64))
}

impl TransferCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        require!(
            client,
            RequirementCheck::KEYPAIR | RequirementCheck::BALANCE
        );

        let from = client.get_payer();
        let from_balance = client.get_balance()?;
        // A wallet holds no data, so this is both the floor a brand-new
        // recipient has to land on and the floor the sender has to stay on.
        let rent_min = client.get_minimum_balance_for_rent_exemption(WALLET_DATA_LEN)?;

        let mut lamports = match self.amount {
            TransferAmount::All => from_balance
                .checked_sub(SIGNATURE_FEE_LAMPORTS)
                .filter(|remaining| *remaining > 0)
                .ok_or_else(|| {
                    eyre::eyre!(
                        "Insufficient funds: {from} holds {} Credits, which does not cover the transaction fee",
                        credits(from_balance)
                    )
                })?,
            TransferAmount::Lamports(lamports) => lamports,
        };

        // The transfer itself creates a recipient that does not exist yet, but
        // the runtime rejects it unless the account lands at or above the
        // rent-exempt minimum, so top the amount up for a new recipient.
        if get_account_balance(client, self.to)? == 0 && lamports < rent_min {
            writeln!(
                out,
                "Recipient {} is not funded yet; sending the rent-exempt minimum of {} Credits to create it",
                self.to,
                credits(rent_min)
            )?;
            lamports = rent_min;
        }

        let required = lamports.saturating_add(SIGNATURE_FEE_LAMPORTS);
        if from_balance < required {
            eyre::bail!(
                "Insufficient funds: {from} holds {} Credits, but the transfer needs {} Credits including the transaction fee",
                credits(from_balance),
                credits(required)
            );
        }

        // The runtime also rejects a transaction that leaves the sender holding
        // a nonzero balance below the rent-exempt minimum, so the leftover has
        // to be either nothing at all or at least that minimum.
        let leftover = from_balance - required;
        if leftover > 0 && leftover < rent_min {
            let most = from_balance.saturating_sub(SIGNATURE_FEE_LAMPORTS + rent_min);
            let advice = if most > 0 {
                format!(
                    "send at most {} Credits, or 'ALL' to empty the account",
                    credits(most)
                )
            } else {
                "send 'ALL' to empty the account".to_string()
            };
            eyre::bail!(
                "Insufficient funds: {from} holds {} Credits, and sending {} Credits would leave {} Credits behind, under the {} Credits an account has to keep to stay rent-exempt; {advice}",
                credits(from_balance),
                credits(lamports),
                credits(leftover),
                credits(rent_min)
            );
        }

        let signature = client.transfer_sol(self.to, lamports)?;

        writeln!(
            out,
            "Transferred {} Credits from {from} to {}",
            credits(lamports),
            self.to
        )?;
        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::doublezerocommand::MockCliCommand;
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use mockall::predicate;
    use solana_sdk::{account::Account, signature::Signature};

    const RENT_MIN: u64 = 890_880;

    // Built from a bare mock rather than `create_test_client`: that helper pins
    // an unbounded `get_balance` expectation, and mockall serves the first
    // matching expectation, so a per-test sender balance could not override it.
    fn client_with(sender_balance: u64, recipient: Option<Account>) -> MockCliCommand {
        let mut client = MockCliCommand::new();
        let payer = Pubkey::from_str_const("DDddB7bhR9azxLAUEH7ZVtW168wRdreiDKhi4McDfKZt");
        client.expect_get_payer().returning(move || payer);
        client.expect_has_keypair_source().returning(|| true);
        client.expect_check_requirements().returning(|_| Ok(()));
        client
            .expect_get_balance()
            .returning(move || Ok(sender_balance));
        client
            .expect_get_multiple_accounts()
            .returning(move |_| Ok(vec![recipient.clone()]));
        client
            .expect_get_minimum_balance_for_rent_exemption()
            .returning(|_| Ok(RENT_MIN));
        client
    }

    fn funded(lamports: u64) -> Option<Account> {
        Some(Account {
            lamports,
            ..Account::default()
        })
    }

    #[test]
    fn test_cli_transfer_to_funded_recipient() {
        let to = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let mut client = client_with(2 * LAMPORTS_PER_CREDIT, funded(RENT_MIN));
        client
            .expect_transfer_sol()
            .with(predicate::eq(to), predicate::eq(LAMPORTS_PER_CREDIT / 2))
            .returning(|_, _| Ok(Signature::default()));

        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let res = block_on(
            TransferCliCommand {
                to,
                amount: TransferAmount::Lamports(LAMPORTS_PER_CREDIT / 2),
            }
            .execute(&ctx, &client, &mut out),
        );

        assert!(res.is_ok());
        let output = String::from_utf8(out).unwrap();
        assert!(output.contains("Transferred 0.5 Credits"));
        assert!(output.contains("Signature:"));
        assert!(!output.contains("not funded yet"));
    }

    // A recipient account that does not exist yet must receive at least the
    // rent-exempt minimum, otherwise the runtime rejects the transfer.
    #[test]
    fn test_cli_transfer_creates_unfunded_recipient() {
        let to = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let mut client = client_with(2 * LAMPORTS_PER_CREDIT, None);
        client
            .expect_transfer_sol()
            .with(predicate::eq(to), predicate::eq(RENT_MIN))
            .returning(|_, _| Ok(Signature::default()));

        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let res = block_on(
            TransferCliCommand {
                to,
                amount: TransferAmount::Lamports(1_000),
            }
            .execute(&ctx, &client, &mut out),
        );

        assert!(res.is_ok());
        let output = String::from_utf8(out).unwrap();
        assert!(output.contains("is not funded yet"));
        assert!(output.contains("0.00089088 Credits"));
    }

    // An amount already above the rent minimum reaches a new account untouched.
    #[test]
    fn test_cli_transfer_unfunded_recipient_keeps_larger_amount() {
        let to = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let mut client = client_with(2 * LAMPORTS_PER_CREDIT, None);
        client
            .expect_transfer_sol()
            .with(predicate::eq(to), predicate::eq(LAMPORTS_PER_CREDIT))
            .returning(|_, _| Ok(Signature::default()));

        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let res = block_on(
            TransferCliCommand {
                to,
                amount: TransferAmount::Lamports(LAMPORTS_PER_CREDIT),
            }
            .execute(&ctx, &client, &mut out),
        );

        assert!(res.is_ok());
        assert!(!String::from_utf8(out).unwrap().contains("not funded yet"));
    }

    #[test]
    fn test_cli_transfer_all_reserves_the_fee() {
        let to = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let mut client = client_with(LAMPORTS_PER_CREDIT, funded(RENT_MIN));
        client
            .expect_transfer_sol()
            .with(
                predicate::eq(to),
                predicate::eq(LAMPORTS_PER_CREDIT - SIGNATURE_FEE_LAMPORTS),
            )
            .returning(|_, _| Ok(Signature::default()));

        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let res = block_on(
            TransferCliCommand {
                to,
                amount: TransferAmount::All,
            }
            .execute(&ctx, &client, &mut out),
        );

        assert!(res.is_ok());
        assert!(String::from_utf8(out)
            .unwrap()
            .contains("Transferred 0.999995 Credits"));
    }

    #[test]
    fn test_cli_transfer_rejects_amount_above_balance() {
        let to = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let client = client_with(LAMPORTS_PER_CREDIT, funded(RENT_MIN));

        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let res = block_on(
            TransferCliCommand {
                to,
                amount: TransferAmount::Lamports(2 * LAMPORTS_PER_CREDIT),
            }
            .execute(&ctx, &client, &mut out),
        );

        let err = res.expect_err("transfer above balance must fail");
        assert!(err.to_string().contains("Insufficient funds"));
    }

    // The fee has to fit on top of the requested amount, not just the amount.
    #[test]
    fn test_cli_transfer_rejects_whole_balance_without_fee_headroom() {
        let to = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let client = client_with(LAMPORTS_PER_CREDIT, funded(RENT_MIN));

        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let res = block_on(
            TransferCliCommand {
                to,
                amount: TransferAmount::Lamports(LAMPORTS_PER_CREDIT),
            }
            .execute(&ctx, &client, &mut out),
        );

        assert!(res.is_err());
    }

    #[test]
    fn test_cli_transfer_all_below_fee_fails() {
        let to = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let client = client_with(SIGNATURE_FEE_LAMPORTS, funded(RENT_MIN));

        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let res = block_on(
            TransferCliCommand {
                to,
                amount: TransferAmount::All,
            }
            .execute(&ctx, &client, &mut out),
        );

        let err = res.expect_err("draining a balance below the fee must fail");
        assert!(err
            .to_string()
            .contains("does not cover the transaction fee"));
    }

    // The runtime rejects a transaction that leaves the sender with a nonzero
    // balance below the rent-exempt minimum: a recipient created at exactly the
    // minimum cannot pass part of it on, only all of it.
    #[test]
    fn test_cli_transfer_rejects_dust_leftover_on_the_sender() {
        let to = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let client = client_with(RENT_MIN, funded(RENT_MIN));

        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let res = block_on(
            TransferCliCommand {
                to,
                amount: TransferAmount::Lamports(10_000),
            }
            .execute(&ctx, &client, &mut out),
        );

        let err = res.expect_err("leaving the sender below the rent minimum must fail");
        let msg = err.to_string();
        assert!(msg.contains("Insufficient funds"));
        assert!(msg.contains("would leave 0.00087588 Credits behind"));
        assert!(msg.contains("send 'ALL' to empty the account"));
    }

    // When a smaller amount would still clear the minimum, say so.
    #[test]
    fn test_cli_transfer_dust_leftover_names_the_largest_payable_amount() {
        let to = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let client = client_with(2 * RENT_MIN, funded(RENT_MIN));

        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let res = block_on(
            TransferCliCommand {
                to,
                amount: TransferAmount::Lamports(RENT_MIN + 500),
            }
            .execute(&ctx, &client, &mut out),
        );

        let err = res.expect_err("leaving the sender below the rent minimum must fail");
        assert!(err
            .to_string()
            .contains("send at most 0.00088588 Credits, or 'ALL' to empty the account"));
    }

    // Leaving nothing behind is fine; only a dust leftover is rejected.
    #[test]
    fn test_cli_transfer_allows_an_exact_drain() {
        let to = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let amount = LAMPORTS_PER_CREDIT - SIGNATURE_FEE_LAMPORTS;
        let mut client = client_with(LAMPORTS_PER_CREDIT, funded(RENT_MIN));
        client
            .expect_transfer_sol()
            .with(predicate::eq(to), predicate::eq(amount))
            .returning(|_, _| Ok(Signature::default()));

        let ctx = cli_context_default_for_tests();
        let mut out = Vec::new();
        let res = block_on(
            TransferCliCommand {
                to,
                amount: TransferAmount::Lamports(amount),
            }
            .execute(&ctx, &client, &mut out),
        );

        assert!(res.is_ok());
        assert!(String::from_utf8(out)
            .unwrap()
            .contains("Transferred 0.999995 Credits"));
    }

    #[test]
    fn test_parse_transfer_amount() {
        assert_eq!(parse_transfer_amount("ALL").unwrap(), TransferAmount::All);
        assert_eq!(parse_transfer_amount("all").unwrap(), TransferAmount::All);
        assert_eq!(
            parse_transfer_amount("1.5").unwrap(),
            TransferAmount::Lamports(1_500_000_000)
        );
        assert_eq!(
            parse_transfer_amount("0.000000001").unwrap(),
            TransferAmount::Lamports(1)
        );
        assert_eq!(
            parse_transfer_amount("0").unwrap(),
            TransferAmount::Lamports(0)
        );
        assert!(parse_transfer_amount("-1").is_err());
        assert!(parse_transfer_amount("abc").is_err());
        assert!(parse_transfer_amount("inf").is_err());
        assert!(parse_transfer_amount("1e30").is_err());
    }
}
