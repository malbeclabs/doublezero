use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_exchange_pda,
    processors::exchange::create::ExchangeCreateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateExchangeCommand {
    pub code: String,
    pub name: String,
    pub lat: f64,
    pub lng: f64,
    pub loc_id: Option<u32>,
}

impl CreateExchangeCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) =
            get_exchange_pda(&client.get_program_id(), globalstate.account_index + 1);
        client
            .execute_transaction(
                DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
                    code: self.code.clone(),
                    name: self.name.clone(),
                    lat: self.lat,
                    lng: self.lng,
                    loc_id: self.loc_id.unwrap_or(0),
                }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::exchange::create::CreateExchangeCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_exchange_pda, get_globalstate_pda},
        processors::exchange::create::ExchangeCreateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_exchange_create_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_exchange_pda(&client.get_program_id(), 1);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
                    code: "test".to_string(),
                    name: "Test Exchange".to_string(),
                    lat: 0.0,
                    lng: 0.0,
                    loc_id: 0,
                })),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = CreateExchangeCommand {
            code: "test".to_string(),
            name: "Test Exchange".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
