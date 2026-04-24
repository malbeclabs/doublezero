use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalconfig_pda, get_metro_pda},
    processors::metro::create::MetroCreateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateMetroCommand {
    pub code: String,
    pub name: String,
    pub lat: f64,
    pub lng: f64,
    pub bgp_community: Option<u16>,
}

impl CreateMetroCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (globalconfig_pubkey, _) = get_globalconfig_pda(&client.get_program_id());
        let (pda_pubkey, _) =
            get_metro_pda(&client.get_program_id(), globalstate.account_index + 1);
        client
            .execute_transaction(
                DoubleZeroInstruction::CreateMetro(MetroCreateArgs {
                    code,
                    name: self.name.clone(),
                    lat: self.lat,
                    lng: self.lng,
                    reserved: 0, // BGP community is auto-assigned
                }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalconfig_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::metro::create::CreateMetroCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalconfig_pda, get_globalstate_pda, get_metro_pda},
        processors::metro::create::MetroCreateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_metro_create_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (globalconfig_pubkey, _) = get_globalconfig_pda(&client.get_program_id());
        let (pda_pubkey, _) = get_metro_pda(&client.get_program_id(), 1);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateMetro(MetroCreateArgs {
                    code: "test_exchange".to_string(),
                    name: "Test Metro".to_string(),
                    lat: 0.0,
                    lng: 0.0,
                    reserved: 0,
                })),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalconfig_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let create_command = CreateMetroCommand {
            code: "test_exchange".to_string(),
            name: "Test Metro".to_string(),
            lat: 0.0,
            lng: 0.0,
            bgp_community: None,
        };

        let create_invalid_command = CreateMetroCommand {
            code: "test/command".to_string(),
            ..create_command.clone()
        };

        let res = create_command.execute(&client);
        assert!(res.is_ok());

        let res = create_invalid_command.execute(&client);
        assert!(res.is_err());
    }
}
