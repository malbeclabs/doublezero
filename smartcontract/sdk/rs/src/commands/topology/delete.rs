use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_topology_pda,
    processors::topology::delete::TopologyDeleteArgs,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteTopologyCommand {
    pub name: String,
}

impl DeleteTopologyCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (topology_pda, _) = get_topology_pda(&client.get_program_id(), &self.name);

        client.execute_transaction(
            DoubleZeroInstruction::DeleteTopology(TopologyDeleteArgs {
                name: self.name.clone(),
            }),
            vec![
                AccountMeta::new(topology_pda, false),
                AccountMeta::new_readonly(globalstate_pubkey, false),
            ],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::topology::delete::DeleteTopologyCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_topology_pda},
        processors::topology::delete::TopologyDeleteArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_topology_delete_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (topology_pda, _) = get_topology_pda(&client.get_program_id(), "unicast-default");

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::DeleteTopology(TopologyDeleteArgs {
                    name: "unicast-default".to_string(),
                })),
                predicate::eq(vec![
                    AccountMeta::new(topology_pda, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = DeleteTopologyCommand {
            name: "unicast-default".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
