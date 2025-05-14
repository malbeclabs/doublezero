use crate::DoubleZeroClient;
use crate::GetGlobalStateCommand;
use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_multicastgroup_pda,
    processors::multicastgroup::update::MulticastGroupUpdateArgs, types::IpV4,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateMulticastGroupCommand {
    pub index: u128,
    pub code: Option<String>,
    pub multicast_ip: Option<IpV4>,
    pub max_bandwidth: Option<u64>,
}

impl UpdateMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, bump_seed) = get_multicastgroup_pda(&client.get_program_id(), self.index);
        client.execute_transaction(
            DoubleZeroInstruction::UpdateMulticastGroup(MulticastGroupUpdateArgs {
                index: self.index,
                bump_seed,
                code: self.code.clone(),
                multicast_ip: self.multicast_ip,
                max_bandwidth: self.max_bandwidth,
            }),
            vec![
                AccountMeta::new(pda_pubkey, false),
                AccountMeta::new(globalstate_pubkey, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::update::UpdateMulticastGroupCommand,
        tests::tests::create_test_client, DoubleZeroClient,
    };
    use doublezero_sla_program::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_location_pda},
        processors::multicastgroup::update::MulticastGroupUpdateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_location_update_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, bump_seed) = get_location_pda(&client.get_program_id(), 1);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::UpdateMulticastGroup(
                    MulticastGroupUpdateArgs {
                        index: 1,
                        bump_seed,
                        code: Some("test".to_string()),
                        multicast_ip: Some([127, 0, 0, 1]),
                        max_bandwidth: Some(1000),
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = UpdateMulticastGroupCommand {
            index: 1,
            code: Some("test".to_string()),
            multicast_ip: Some([127, 0, 0, 1]),
            max_bandwidth: Some(1000),
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
