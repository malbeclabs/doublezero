use crate::{
    commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient, GetFacilityCommand,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::facility::delete::FacilityDeleteArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteFacilityCommand {
    pub pubkey: Pubkey,
}

impl DeleteFacilityCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, facility) = GetFacilityCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Facility not found"))?;

        if facility.reference_count > 0 {
            return Err(eyre::eyre!(
                "Facility cannot be deleted, it has {} references",
                facility.reference_count
            ));
        }

        client.execute_transaction(
            DoubleZeroInstruction::DeleteFacility(FacilityDeleteArgs {}),
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
        commands::facility::delete::DeleteFacilityCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_facility_pda, get_globalstate_pda},
        processors::facility::delete::FacilityDeleteArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            facility::{Facility, FacilityStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_facility_delete_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_facility_pda(&client.get_program_id(), 1);
        let facility = Facility {
            account_type: AccountType::Facility,
            index: 1,
            bump_seed: 255,
            code: "loc".to_string(),
            name: "Test Facility".to_string(),
            country: "Test Country".to_string(),
            reference_count: 0,
            owner: Pubkey::default(),
            lat: 0.0,
            lng: 0.0,
            loc_id: 123,
            status: FacilityStatus::Activated,
        };

        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| Ok(AccountData::Facility(facility.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteFacility(FacilityDeleteArgs {})),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteFacilityCommand { pubkey: pda_pubkey }.execute(&client);

        assert!(res.is_ok());
    }
}
