use crate::{
    commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient, GetLocationCommand,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::location::delete::LocationDeleteArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteLocationCommand {
    pub pubkey: Pubkey,
}

impl DeleteLocationCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

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

        client.execute_transaction(
            DoubleZeroInstruction::DeleteLocation(LocationDeleteArgs {}),
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
        commands::location::delete::DeleteLocationCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_location_pda},
        processors::location::delete::LocationDeleteArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            location::{Location, LocationStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_location_delete_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_location_pda(&client.get_program_id(), 1);
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

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteLocation(LocationDeleteArgs {})),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteLocationCommand { pubkey: pda_pubkey }.execute(&client);

        assert!(res.is_ok());
    }
}
