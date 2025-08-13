use doublezero_program_common::validate_account_code;
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
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) =
            get_exchange_pda(&client.get_program_id(), globalstate.account_index + 1);
        client
            .execute_transaction(
                DoubleZeroInstruction::CreateExchange(ExchangeCreateArgs {
                    code,
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
                    code: "test_exchange".to_string(),
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

        let create_command = CreateExchangeCommand {
            code: "test_exchange".to_string(),
            name: "Test Exchange".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: None,
        };

        let create_invalid_command = CreateExchangeCommand {
            code: "test/command".to_string(),
            ..create_command.clone()
        };

        let res = create_command.execute(&client);
        assert!(res.is_ok());

        let res = create_invalid_command.execute(&client);
        assert!(res.is_err());
    }
}
