use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_exchange_pda,
    processors::exchange::update::ExchangeUpdateArgs,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateExchangeCommand {
    pub index: u128,
    pub code: Option<String>,
    pub name: Option<String>,
    pub lat: Option<f64>,
    pub lng: Option<f64>,
    pub loc_id: Option<u32>,
}

impl UpdateExchangeCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, bump_seed) = get_exchange_pda(&client.get_program_id(), self.index);
        client.execute_transaction(
            DoubleZeroInstruction::UpdateExchange(ExchangeUpdateArgs {
                index: self.index,
                bump_seed,
                code: self.code.to_owned(),
                name: self.name.to_owned(),
                lat: self.lat,
                lng: self.lng,
                loc_id: self.loc_id,
            }),
            vec![
                AccountMeta::new(pda_pubkey, false),
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
    use solana_sdk::{instruction::AccountMeta, signature::Signature, system_program};

    #[test]
    fn test_commands_exchange_update_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, bump_seed) = get_exchange_pda(&client.get_program_id(), 1);
        let payer = client.get_payer();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateExchange(ExchangeUpdateArgs {
                    index: 1,
                    bump_seed,
                    code: Some("test".to_string()),
                    name: Some("Test Exchange".to_string()),
                    lat: Some(0.0),
                    lng: Some(0.0),
                    loc_id: Some(0),
                })),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(payer, true),
                    AccountMeta::new(system_program::id(), false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = UpdateExchangeCommand {
            index: 1,
            code: Some("test".to_string()),
            name: Some("Test Exchange".to_string()),
            lat: Some(0.0),
            lng: Some(0.0),
            loc_id: Some(0),
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
