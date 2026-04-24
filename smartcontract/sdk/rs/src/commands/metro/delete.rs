use crate::{
    commands::{globalstate::get::GetGlobalStateCommand, metro::get::GetMetroCommand},
    DoubleZeroClient,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::metro::delete::MetroDeleteArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteMetroCommand {
    pub pubkey: Pubkey,
}

impl DeleteMetroCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (_, metro) = GetMetroCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Metro not found"))?;

        if metro.reference_count > 0 {
            return Err(eyre::eyre!(
                "Metro cannot be deleted, it has {} references",
                metro.reference_count
            ));
        }

        client.execute_transaction(
            DoubleZeroInstruction::DeleteMetro(MetroDeleteArgs {}),
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
        commands::metro::delete::DeleteMetroCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_metro_pda},
        processors::metro::delete::MetroDeleteArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            metro::{Metro, MetroStatus},
        },
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_metro_delete_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_metro_pda(&client.get_program_id(), 1);
        let metro = Metro {
            account_type: AccountType::Metro,
            index: 1,
            bump_seed: 255,
            code: "loc".to_string(),
            name: "Test Facility".to_string(),
            device1_pk: Pubkey::default(),
            device2_pk: Pubkey::default(),
            reference_count: 0,
            owner: Pubkey::default(),
            lat: 0.0,
            lng: 0.0,
            bgp_community: 123,
            unused: 0,
            status: MetroStatus::Activated,
        };

        client
            .expect_get()
            .with(predicate::eq(pda_pubkey))
            .returning(move |_| Ok(AccountData::Metro(metro.clone())));

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteMetro(MetroDeleteArgs {})),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteMetroCommand { pubkey: pda_pubkey }.execute(&client);

        assert!(res.is_ok());
    }
}
