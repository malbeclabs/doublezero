use crate::DoubleZeroClient;
use doublezero_serviceability::processors::globalstate::setauthority::SetAuthorityArgs;
use doublezero_serviceability_instruction::globalstate::set_authority;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct SetAuthorityCommand {
    pub activator_authority_pk: Option<Pubkey>,
    pub sentinel_authority_pk: Option<Pubkey>,
    pub health_oracle_pk: Option<Pubkey>,
    pub feed_authority_pk: Option<Pubkey>,
}

impl SetAuthorityCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(set_authority(
            &client.get_program_id(),
            &client.get_payer(),
            SetAuthorityArgs {
                activator_authority_pk: self.activator_authority_pk,
                sentinel_authority_pk: self.sentinel_authority_pk,
                health_oracle_pk: self.health_oracle_pk,
                feed_authority_pk: self.feed_authority_pk,
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::globalstate::setauthority::SetAuthorityCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::processors::globalstate::setauthority::SetAuthorityArgs;
    use doublezero_serviceability_instruction::globalstate::set_authority;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_setauthority_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();

        let activator_authority_pk = Pubkey::new_unique();
        let sentinel_authority_pk = Pubkey::new_unique();
        let health_oracle_pk = Pubkey::new_unique();
        let feed_authority_pk = Pubkey::new_unique();

        let expected = set_authority(
            &program_id,
            &payer,
            SetAuthorityArgs {
                activator_authority_pk: Some(activator_authority_pk),
                sentinel_authority_pk: Some(sentinel_authority_pk),
                health_oracle_pk: Some(health_oracle_pk),
                feed_authority_pk: Some(feed_authority_pk),
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = SetAuthorityCommand {
            activator_authority_pk: Some(activator_authority_pk),
            sentinel_authority_pk: Some(sentinel_authority_pk),
            health_oracle_pk: Some(health_oracle_pk),
            feed_authority_pk: Some(feed_authority_pk),
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
