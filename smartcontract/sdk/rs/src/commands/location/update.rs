use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::location::update::LocationUpdateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateLocationCommand {
    pub pubkey: Pubkey,
    pub code: Option<String>,
    pub name: Option<String>,
    pub country: Option<String>,
    pub lat: Option<f64>,
    pub lng: Option<f64>,
    pub loc_id: Option<u32>,
}

impl UpdateLocationCommand {
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
            DoubleZeroInstruction::UpdateLocation(LocationUpdateArgs {
                code,
                name: self.name.to_owned(),
                country: self.country.to_owned(),
                lat: self.lat,
                lng: self.lng,
                loc_id: self.loc_id,
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
        commands::location::update::UpdateLocationCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_location_pda},
        processors::location::update::LocationUpdateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_location_update_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_location_pda(&client.get_program_id(), 1);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateLocation(LocationUpdateArgs {
                    code: Some("test_location".to_string()),
                    name: Some("Test Location".to_string()),
                    country: Some("Test Country".to_string()),
                    lat: Some(0.0),
                    lng: Some(0.0),
                    loc_id: Some(0),
                })),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let update_command = UpdateLocationCommand {
            pubkey: pda_pubkey,
            code: Some("test_location".to_string()),
            name: Some("Test Location".to_string()),
            country: Some("Test Country".to_string()),
            lat: Some(0.0),
            lng: Some(0.0),
            loc_id: Some(0),
        };

        let update_invalid_command = UpdateLocationCommand {
            code: Some("test/location".to_string()),
            ..update_command.clone()
        };

        let res = update_command.execute(&client);
        assert!(res.is_ok());

        let res = update_invalid_command.execute(&client);
        assert!(res.is_err());
    }
}
