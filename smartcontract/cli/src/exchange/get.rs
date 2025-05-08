use clap::Args;
use doublezero_sdk::commands::exchange::get::GetExchangeCommand;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetExchangeCliCommand {
    #[arg(long)]
    pub code: String,
}

impl GetExchangeCliCommand {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        let (pubkey, exchange) = GetExchangeCommand {
            pubkey_or_code: self.code,
        }
        .execute(client)?;

        writeln!(out, 
                "account: {},\r\ncode: {}\r\nname: {}\r\nlat: {}\r\nlng: {}\r\nloc_id: {}\r\nstatus: {}\r\nowner: {}",
                pubkey,
                exchange.code,
                exchange.name,
                exchange.lat,
                exchange.lng,
                exchange.loc_id,
                exchange.status,
                exchange.owner
            )?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;
    use std::str::FromStr;
    use doublezero_sdk::{AccountData, AccountType, Exchange, ExchangeStatus};
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use crate::exchange::get::GetExchangeCliCommand;
    use crate::tests::tests::create_test_client;

    #[test]
    fn test_cli_exchange_get() {
        let mut client = create_test_client();

        let exchange1_pubkey = Pubkey::from_str("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB").unwrap();
        let exchange1 = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 255,
            code: "test".to_string(),
            name: "Test Exchange".to_string(),
            lat: 12.34,
            lng: 56.78,
            loc_id: 1,
            status: ExchangeStatus::Activated,
            owner: exchange1_pubkey,
        };

        let exchange2 = exchange1.clone();
        client
            .expect_get()
            .with(predicate::eq(exchange1_pubkey))
            .returning(move |_| Ok(AccountData::Exchange(exchange2.clone())));
        client
            .expect_get()
            .returning(move |_| Err(eyre::eyre!("not found")));

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Exchange))
            .returning(move |_| {
                let mut list = HashMap::new();
                list.insert(exchange1_pubkey, AccountData::Exchange(exchange1.clone()));
                Ok(list)
            });

        // Expected failure
        let mut output = Vec::new();
        let res = GetExchangeCliCommand {
            code: Pubkey::new_unique().to_string(),
        }
        .execute(&client, &mut output);
        assert!(!res.is_ok());

        // Expected success
        let mut output = Vec::new();
        let res = GetExchangeCliCommand {
            code: exchange1_pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB,\r\ncode: test\r\nname: Test Exchange\r\nlat: 12.34\r\nlng: 56.78\r\nloc_id: 1\r\nstatus: activated\r\nowner: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB\n");

        // Expected success 
        let mut output = Vec::new();
        let res = GetExchangeCliCommand {
            code: "test".to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "account: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB,\r\ncode: test\r\nname: Test Exchange\r\nlat: 12.34\r\nlng: 56.78\r\nloc_id: 1\r\nstatus: activated\r\nowner: BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB\n");

    }
}
