use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::exchange::update::ExchangeUpdateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateExchangeCommand {
    pub pubkey: Pubkey,
    pub code: Option<String>,
    pub name: Option<String>,
    pub lat: Option<f64>,
    pub lng: Option<f64>,
    pub bgp_community: Option<u32>,
}

impl UpdateExchangeCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let code = self
            .code
            .as_ref()
            .map(|code| validate_account_code(code))
            .transpose()
            .map_err(|err| eyre::eyre!("invalid code: {err}"))?;
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        client.execute_transaction(
            DoubleZeroInstruction::UpdateExchange(ExchangeUpdateArgs {
                code,
                name: self.name.to_owned(),
                lat: self.lat,
                lng: self.lng,
                bgp_community: self.bgp_community,
            }),
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
        commands::exchange::update::UpdateExchangeCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_exchange_pda, get_globalstate_pda},
        processors::exchange::update::ExchangeUpdateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_exchange_update_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_exchange_pda(&client.get_program_id(), 1);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateExchange(ExchangeUpdateArgs {
                    code: Some("test_exchange".to_string()),
                    name: Some("Test Exchange".to_string()),
                    lat: Some(0.0),
                    lng: Some(0.0),
                    bgp_community: Some(0),
                })),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let update_command = UpdateExchangeCommand {
            pubkey: pda_pubkey,
            code: Some("test_exchange".to_string()),
            name: Some("Test Exchange".to_string()),
            lat: Some(0.0),
            lng: Some(0.0),
            bgp_community: Some(0),
        };

        let update_invalid_command = UpdateExchangeCommand {
            code: Some("test/exchange".to_string()),
            ..update_command.clone()
        };

        let res = update_command.execute(&client);
        assert!(res.is_ok());
        let res = update_invalid_command.execute(&client);
        assert!(res.is_err());
    }
}
