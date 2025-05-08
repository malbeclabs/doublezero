use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::exchange::delete::DeleteExchangeCommand;
use doublezero_sdk::commands::exchange::get::GetExchangeCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct DeleteExchangeCliCommand {
    #[arg(long)]
    pub pubkey: String,
}

impl DeleteExchangeCliCommand {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let (_, exchange) = GetExchangeCommand {
            pubkey_or_code: self.pubkey,
        }
        .execute(client)?;
        let signature = DeleteExchangeCommand {
            index: exchange.index,
        }
        .execute(client)?;
        writeln!(out, "Signature: {}", signature)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::exchange::delete::DeleteExchangeCliCommand;
    use crate::tests::tests::create_test_client;
    use doublezero_sdk::get_exchange_pda;
    use doublezero_sdk::AccountData;
    use doublezero_sdk::AccountType;
    use doublezero_sdk::DoubleZeroClient;
    use doublezero_sdk::Exchange;
    use doublezero_sdk::ExchangeStatus;
    use doublezero_sla_program::instructions::DoubleZeroInstruction;
    use doublezero_sla_program::pda::get_globalstate_pda;
    use doublezero_sla_program::processors::exchange::delete::ExchangeDeleteArgs;
    use mockall::predicate;
    use solana_sdk::instruction::AccountMeta;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_cli_exchange_delete() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, bump_seed) = get_exchange_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let exchange = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            name: "Test Exchange".to_string(),
            lat: 12.34,
            lng: 56.78,
            loc_id: 1,
            status: ExchangeStatus::Activated,
            owner: pda_pubkey,
        };

        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| Ok(AccountData::Exchange(exchange.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteExchange(ExchangeDeleteArgs {
                    index: 1,
                    bump_seed,
                })),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(move |_, _| Ok(signature));

        let mut output = Vec::new();
        let res = DeleteExchangeCliCommand {
            pubkey: pda_pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
