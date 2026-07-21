use crate::DoubleZeroClient;
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    pda::get_tenant_pda, processors::tenant::create::TenantCreateArgs,
};
use doublezero_serviceability_instruction::tenant::create_tenant;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateTenantCommand {
    pub code: String,
    pub administrator: Pubkey,
    pub token_account: Option<Pubkey>,
    pub metro_routing: bool,
    pub route_liveness: bool,
}

impl CreateTenantCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let program_id = client.get_program_id();
        let (pda_pubkey, _) = get_tenant_pda(&program_id, &code);

        let ix = create_tenant(
            &program_id,
            &client.get_payer(),
            TenantCreateArgs {
                code,
                administrator: self.administrator,
                token_account: self.token_account,
                metro_routing: self.metro_routing,
                route_liveness: self.route_liveness,
            },
        );

        client.send_transaction(ix).map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::tenant::create::CreateTenantCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::processors::tenant::create::TenantCreateArgs;
    use doublezero_serviceability_instruction::tenant::create_tenant;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_tenant_create_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let administrator = Pubkey::new_unique();

        let expected = create_tenant(
            &program_id,
            &payer,
            TenantCreateArgs {
                code: "test".to_string(),
                administrator,
                token_account: None,
                metro_routing: true,
                route_liveness: false,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = CreateTenantCommand {
            code: "test/invalid".to_string(),
            administrator: Pubkey::default(),
            token_account: None,
            metro_routing: true,
            route_liveness: false,
        }
        .execute(&client);

        assert!(res.is_err());

        let res = CreateTenantCommand {
            code: "test".to_string(),
            administrator,
            token_account: None,
            metro_routing: true,
            route_liveness: false,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
