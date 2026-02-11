use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_resource_extension_pda, get_tenant_pda},
    processors::tenant::create::TenantCreateArgs,
    resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateTenantCommand {
    pub code: String,
    pub administrator: Pubkey,
    pub token_account: Option<Pubkey>,
    pub metro_route: bool,
    pub route_aliveness: bool,
}

impl CreateTenantCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) = get_tenant_pda(&client.get_program_id(), &code);
        let (vrf_ids_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::VrfIds);
        client
            .execute_transaction(
                DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
                    code,
                    administrator: self.administrator,
                    token_account: self.token_account,
                    metro_route: self.metro_route,
                    route_aliveness: self.route_aliveness,
                }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                    AccountMeta::new(vrf_ids_pda, false),
                ],
            )
            .map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{commands::tenant::create::CreateTenantCommand, tests::utils::create_test_client};
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction, processors::tenant::create::TenantCreateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_tenant_create_command() {
        let mut client = create_test_client();

        let administrator = Pubkey::new_unique();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateTenant(TenantCreateArgs {
                    code: "test".to_string(),
                    administrator,
                    token_account: None,
                    metro_route: true,
                    route_aliveness: false,
                })),
                predicate::always(),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = CreateTenantCommand {
            code: "test/invalid".to_string(),
            administrator: Pubkey::default(),
            token_account: None,
            metro_route: true,
            route_aliveness: false,
        }
        .execute(&client);

        assert!(res.is_err());

        let res = CreateTenantCommand {
            code: "test".to_string(),
            administrator,
            token_account: None,
            metro_route: true,
            route_aliveness: false,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
