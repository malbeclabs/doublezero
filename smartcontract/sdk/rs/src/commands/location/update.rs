use double_zero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_location_pda,
    processors::location::update::LocationUpdateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::accountdata::getglobalstate::GetGlobalStateCommand, DoubleZeroClient};

pub struct UpdateLocationCommand {
    pub index: u128,
    pub code: Option<String>,
    pub name: Option<String>,
    pub country: Option<String>,
    pub lat: Option<f64>,
    pub lng: Option<f64>,
    pub loc_id: Option<u32>,
}

impl UpdateLocationCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) = get_location_pda(&client.get_program_id(), self.index);
        client
            .execute_transaction(
                DoubleZeroInstruction::UpdateLocation(LocationUpdateArgs {
                    index: self.index,
                    code: self.code.to_owned(),
                    name: self.name.to_owned(),
                    country: self.country.to_owned(),
                    lat: self.lat,
                    lng: self.lng,
                    loc_id: self.loc_id,
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
        commands::location::update::UpdateLocationCommand, tests::tests::create_test_client,
        DoubleZeroClient,
    };
    use double_zero_sla_program::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_location_pda},
        processors::location::update::LocationUpdateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature, system_program};

    #[test]
    fn test_commands_location_update_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_location_pda(&client.get_program_id(), 1);
        let payer = client.get_payer();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateLocation(LocationUpdateArgs {
                    index: 1,
                    code: Some("test".to_string()),
                    name: Some("Test Location".to_string()),
                    country: Some("Test Country".to_string()),
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

        let res = UpdateLocationCommand {
            index: 1,
            code: Some("test".to_string()),
            name: Some("Test Location".to_string()),
            country: Some("Test Country".to_string()),
            lat: Some(0.0),
            lng: Some(0.0),
            loc_id: Some(0),
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
