use crate::{commands::exchange::get::GetExchangeCommand, DoubleZeroClient};
use doublezero_serviceability::processors::exchange::delete::ExchangeDeleteArgs;
use doublezero_serviceability_instruction::exchange::delete_exchange;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteExchangeCommand {
    pub pubkey: Pubkey,
}

impl DeleteExchangeCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (_, exchange) = GetExchangeCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Exchange not found"))?;

        if exchange.reference_count > 0 {
            return Err(eyre::eyre!(
                "Exchange cannot be deleted, it has {} references",
                exchange.reference_count
            ));
        }

        client.send_transaction(delete_exchange(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            ExchangeDeleteArgs {},
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::exchange::delete::DeleteExchangeCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_exchange_pda,
        processors::exchange::delete::ExchangeDeleteArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            exchange::{Exchange, ExchangeStatus},
        },
    };
    use doublezero_serviceability_instruction::exchange::delete_exchange;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_exchange_delete_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (pda_pubkey, _) = get_exchange_pda(&program_id, 1);
        let exchange = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 255,
            code: "loc".to_string(),
            name: "Test Location".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            reference_count: 0,
            owner: Pubkey::default(),
            lat: 0.0,
            lng: 0.0,
            bgp_community: 123,
            unused: 0,
            status: ExchangeStatus::Activated,
        };

        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| Ok(AccountData::Exchange(exchange.clone())));

        let expected = delete_exchange(&program_id, &payer, &pda_pubkey, ExchangeDeleteArgs {});
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = DeleteExchangeCommand { pubkey: pda_pubkey }.execute(&client);

        assert!(res.is_ok());
    }
}
