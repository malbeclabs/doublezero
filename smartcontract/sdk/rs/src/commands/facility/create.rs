use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_facility_pda,
    processors::facility::create::FacilityCreateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateFacilityCommand {
    pub code: String,
    pub name: String,
    pub country: String,
    pub lat: f64,
    pub lng: f64,
    pub loc_id: Option<u32>,
}

impl CreateFacilityCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) =
            get_facility_pda(&client.get_program_id(), globalstate.account_index + 1);
        client
            .execute_transaction(
                DoubleZeroInstruction::CreateFacility(FacilityCreateArgs {
                    code,
                    name: self.name.clone(),
                    country: self.country.clone(),
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
    use crate::{tests::utils::create_test_client, CreateFacilityCommand, DoubleZeroClient};
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_facility_pda, get_globalstate_pda},
        processors::facility::create::FacilityCreateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_facility_create_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_facility_pda(&client.get_program_id(), 1);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateFacility(FacilityCreateArgs {
                    code: "test_location".to_string(),
                    name: "Test Facility".to_string(),
                    country: "Test Country".to_string(),
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

        let create_command = CreateFacilityCommand {
            code: "test_location".to_string(),
            name: "Test Facility".to_string(),
            country: "Test Country".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: None,
        };

        let create_invalid_command = CreateFacilityCommand {
            code: "test/location".to_string(),
            ..create_command.clone()
        };

        let res = create_command.execute(&client);
        assert!(res.is_ok());

        let res = create_invalid_command.execute(&client);
        assert!(res.is_err());
    }
}
