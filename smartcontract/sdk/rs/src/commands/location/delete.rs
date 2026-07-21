use crate::{DoubleZeroClient, GetLocationCommand};
use doublezero_serviceability::processors::location::delete::LocationDeleteArgs;
use doublezero_serviceability_instruction::location::delete_location;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteLocationCommand {
    pub pubkey: Pubkey,
}

impl DeleteLocationCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (_, location) = GetLocationCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Location not found"))?;

        if location.reference_count > 0 {
            return Err(eyre::eyre!(
                "Location cannot be deleted, it has {} references",
                location.reference_count
            ));
        }

        client.send_transaction(delete_location(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            LocationDeleteArgs {},
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::location::delete::DeleteLocationCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        pda::get_location_pda,
        processors::location::delete::LocationDeleteArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            location::{Location, LocationStatus},
        },
    };
    use doublezero_serviceability_instruction::location::delete_location;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_location_delete_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (pda_pubkey, _) = get_location_pda(&program_id, 1);
        let location = Location {
            account_type: AccountType::Location,
            index: 1,
            bump_seed: 255,
            code: "loc".to_string(),
            name: "Test Location".to_string(),
            country: "Test Country".to_string(),
            reference_count: 0,
            owner: Pubkey::default(),
            lat: 0.0,
            lng: 0.0,
            loc_id: 123,
            status: LocationStatus::Activated,
        };

        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| Ok(AccountData::Location(location.clone())));

        let expected = delete_location(&program_id, &payer, &pda_pubkey, LocationDeleteArgs {});
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = DeleteLocationCommand { pubkey: pda_pubkey }.execute(&client);

        assert!(res.is_ok());
    }
}
