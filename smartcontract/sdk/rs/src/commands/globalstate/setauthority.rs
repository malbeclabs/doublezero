use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::globalstate::setauthority::SetAuthorityArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct SetAuthorityCommand {
    pub activator_authority_pk: Option<Pubkey>,
    pub sentinel_authority_pk: Option<Pubkey>,
    pub health_oracle_pk: Option<Pubkey>,
}

impl SetAuthorityCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        client.execute_transaction(
            DoubleZeroInstruction::SetAuthority(SetAuthorityArgs {
                activator_authority_pk: self.activator_authority_pk,
                sentinel_authority_pk: self.sentinel_authority_pk,
                health_oracle_pk: self.health_oracle_pk,
                reservation_authority_pk: None,
            }),
            vec![AccountMeta::new(globalstate_pubkey, false)],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::globalstate::setauthority::SetAuthorityCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction, pda::get_globalstate_pda,
        processors::globalstate::setauthority::SetAuthorityArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_setauthority_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());

        let activator_authority_pk = Pubkey::new_unique();
        let sentinel_authority_pk = Pubkey::new_unique();
        let health_oracle_pk = Pubkey::new_unique();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SetAuthority(SetAuthorityArgs {
                    activator_authority_pk: Some(activator_authority_pk),
                    sentinel_authority_pk: Some(sentinel_authority_pk),
                    health_oracle_pk: Some(health_oracle_pk),
                    reservation_authority_pk: None,
                })),
                predicate::eq(vec![AccountMeta::new(globalstate_pubkey, false)]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SetAuthorityCommand {
            activator_authority_pk: Some(activator_authority_pk),
            sentinel_authority_pk: Some(sentinel_authority_pk),
            health_oracle_pk: Some(health_oracle_pk),
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
