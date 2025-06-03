use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::exchange::get::GetExchangeCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetExchangeCliCommand {
    #[arg(long)]
    pub code: String,
}

impl GetExchangeCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (pubkey, exchange) = client.get_exchange(GetExchangeCommand {
            pubkey_or_code: self.code,
        })?;

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
    use crate::exchange::get::GetExchangeCliCommand;
    use crate::tests::utils::create_test_client;
    use doublezero_sdk::commands::exchange::get::GetExchangeCommand;
    use doublezero_sdk::{AccountType, Exchange, ExchangeStatus};
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;
    use std::collections::HashMap;
    use std::str::FromStr;

    #[test]
    fn test_cli_exchange_get() {
        let mut client = create_test_client();

        let exchange1_pubkey =
            Pubkey::from_str("BmrLoL9jzYo4yiPUsFhYFU8hgE3CD3Npt8tgbqvneMyB").unwrap();
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
            .expect_get_exchange()
            .with(predicate::eq(GetExchangeCommand {
                pubkey_or_code: exchange1_pubkey.to_string(),
            }))
            .returning(move |_| Ok((exchange1_pubkey, exchange2.clone())));
        let exchange3 = exchange1.clone();
        client
            .expect_get_exchange()
            .with(predicate::eq(GetExchangeCommand {
                pubkey_or_code: "test".to_string(),
            }))
            .returning(move |_| Ok((exchange1_pubkey, exchange3.clone())));
        client
            .expect_get_exchange()
            .returning(move |_| Err(eyre::eyre!("not found")));

        client.expect_list_exchange().returning(move |_| {
            let mut list = HashMap::new();
            list.insert(exchange1_pubkey, exchange1.clone());
            Ok(list)
        });

        // Expected failure
        let mut output = Vec::new();
        let res = GetExchangeCliCommand {
            code: Pubkey::new_unique().to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_err());

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
