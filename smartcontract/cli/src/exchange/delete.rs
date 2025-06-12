use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::commands::exchange::{delete::DeleteExchangeCommand, get::GetExchangeCommand};
use std::io::Write;

#[derive(Args, Debug)]
pub struct DeleteExchangeCliCommand {
    /// Exchange Pubkey or code to delete
    #[arg(long)]
    pub pubkey: String,
}

impl DeleteExchangeCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (_, exchange) = client.get_exchange(GetExchangeCommand {
            pubkey_or_code: self.pubkey,
        })?;
        let signature = client.delete_exchange(DeleteExchangeCommand {
            index: exchange.index,
        })?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand,
        exchange::delete::DeleteExchangeCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::exchange::{delete::DeleteExchangeCommand, get::GetExchangeCommand},
        get_exchange_pda, AccountType, Exchange, ExchangeStatus,
    };
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_cli_exchange_delete() {
        let mut client = create_test_client();

        let (pda_pubkey, _bump_seed) = get_exchange_pda(&client.get_program_id(), 1);
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
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_exchange()
            .with(predicate::eq(GetExchangeCommand {
                pubkey_or_code: pda_pubkey.to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, exchange.clone())));

        client
            .expect_delete_exchange()
            .with(predicate::eq(DeleteExchangeCommand { index: 1 }))
            .times(1)
            .returning(move |_| Ok(signature));

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
