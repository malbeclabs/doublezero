use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_location_pda,
    processors::location::create::LocationCreateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

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
        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, bump_seed) =
            get_location_pda(&client.get_program_id(), globalstate.account_index + 1);
        client
            .execute_transaction(
                DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
                    index: globalstate.account_index + 1,
                    bump_seed,
                    code: self.code.clone(),
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
    use crate::{tests::tests::create_test_client, CreateLocationCommand, DoubleZeroClient};
    use doublezero_sla_program::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_location_pda},
        processors::location::create::LocationCreateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_location_create_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, bump_seed) = get_location_pda(&client.get_program_id(), 1);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateLocation(LocationCreateArgs {
                    index: 1,
                    bump_seed,
                    code: "test".to_string(),
                    name: "Test Location".to_string(),
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

        let res = CreateLocationCommand {
            code: "test".to_string(),
            name: "Test Location".to_string(),
            country: "Test Country".to_string(),
            lat: 0.0,
            lng: 0.0,
            loc_id: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
