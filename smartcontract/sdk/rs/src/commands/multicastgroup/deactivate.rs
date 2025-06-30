use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::multicastgroup::deactivate::MulticastGroupDeactivateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeactivateMulticastGroupCommand {
    pub pubkey: Pubkey,
    pub owner: Pubkey,
}

impl DeactivateMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        client.execute_transaction(
            DoubleZeroInstruction::DeactivateMulticastGroup(MulticastGroupDeactivateArgs {}),
            vec![
                AccountMeta::new(self.pubkey, false),
                AccountMeta::new(self.owner, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::deactivate::DeactivateMulticastGroupCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_location_pda},
        processors::multicastgroup::deactivate::MulticastGroupDeactivateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_location_deactivate_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, bump_seed) = get_location_pda(&client.get_program_id(), 1);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeactivateMulticastGroup(
                    MulticastGroupDeactivateArgs {},
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let payer = client.get_payer();
        let res = DeactivateMulticastGroupCommand {
            pubkey: pda_pubkey,
            owner: payer,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
