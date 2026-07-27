use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    pda::get_location_pda, processors::location::create::LocationCreateArgs,
};
use doublezero_serviceability_instruction::location::create_location;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateLocationCommand {
    pub code: String,
    pub name: String,
    pub country: String,
    pub lat: f64,
    pub lng: f64,
    pub loc_id: Option<u32>,
}

impl CreateLocationCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let (_, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let program_id = client.get_program_id();
        let account_index = globalstate.account_index + 1;
        let (pda_pubkey, _) = get_location_pda(&program_id, account_index);

        let ix = create_location(
            &program_id,
            &client.get_payer(),
            account_index,
            LocationCreateArgs {
                code,
                name: self.name.clone(),
                country: self.country.clone(),
                lat: self.lat,
                lng: self.lng,
                loc_id: self.loc_id.unwrap_or(0),
            },
        );

        client.send_transaction(ix).map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{tests::utils::create_test_client, CreateLocationCommand, DoubleZeroClient};
    use doublezero_serviceability::processors::location::create::LocationCreateArgs;
    use doublezero_serviceability_instruction::location::create_location;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_commands_location_create_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();

        // create_test_client seeds globalstate.account_index = 0, so the new
        // location index is 1. The command must hand send_transaction exactly the
        // builder's instruction.
        let expected = create_location(
            &program_id,
            &payer,
            1,
            LocationCreateArgs {
                code: "test_location".to_string(),
                name: "Test Location".to_string(),
                country: "Test Country".to_string(),
                lat: 0.0,
                lng: 0.0,
                loc_id: 0,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let create_command = CreateLocationCommand {
            code: "test_location".to_string(),
            name: "Test Location".to_string(),
            country: "Test Country".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: None,
        };

        let create_invalid_command = CreateLocationCommand {
            code: "test/location".to_string(),
            ..create_command.clone()
        };

        let res = create_command.execute(&client);
        assert!(res.is_ok());

        let res = create_invalid_command.execute(&client);
        assert!(res.is_err());
    }
}
