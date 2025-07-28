use crate::{
    commands::{exchange::get::GetExchangeCommand, globalstate::get::GetGlobalStateCommand},
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::exchange::delete::ExchangeDeleteArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteExchangeCommand {
    pub pubkey: Pubkey,
}

impl DeleteExchangeCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

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

        client.execute_transaction(
            DoubleZeroInstruction::DeleteExchange(ExchangeDeleteArgs {}),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::exchange::delete::DeleteExchangeCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_exchange_pda, get_globalstate_pda},
        processors::exchange::delete::ExchangeDeleteArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            exchange::{Exchange, ExchangeStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_exchange_delete_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_exchange_pda(&client.get_program_id(), 1);
        let exchange = Exchange {
            account_type: AccountType::Exchange,
            index: 1,
            bump_seed: 255,
            code: "loc".to_string(),
            name: "Test Location".to_string(),
            reference_count: 0,
            owner: Pubkey::default(),
            lat: 0.0,
            lng: 0.0,
            loc_id: 123,
            status: ExchangeStatus::Activated,
        };

        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| Ok(AccountData::Exchange(exchange.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteExchange(ExchangeDeleteArgs {})),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteExchangeCommand { pubkey: pda_pubkey }.execute(&client);

        assert!(res.is_ok());
    }
}
