use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_topology_pda,
    processors::topology::clear::TopologyClearArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct ClearTopologyCommand {
    pub name: String,
    pub link_pubkeys: Vec<Pubkey>,
}

impl ClearTopologyCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (topology_pda, _) = get_topology_pda(&client.get_program_id(), &self.name);

        let mut accounts = vec![
            AccountMeta::new_readonly(topology_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ];

        for link_pk in &self.link_pubkeys {
            accounts.push(AccountMeta::new(*link_pk, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::ClearTopology(TopologyClearArgs {
                name: self.name.clone(),
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::topology::clear::ClearTopologyCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_topology_pda},
        processors::topology::clear::TopologyClearArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_topology_clear_command_no_links() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (topology_pda, _) = get_topology_pda(&client.get_program_id(), "my-topology");

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::ClearTopology(TopologyClearArgs {
                    name: "my-topology".to_string(),
                })),
                predicate::eq(vec![
                    AccountMeta::new_readonly(topology_pda, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = ClearTopologyCommand {
            name: "my-topology".to_string(),
            link_pubkeys: vec![],
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_topology_clear_command_with_links() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (topology_pda, _) = get_topology_pda(&client.get_program_id(), "my-topology");
        let link1 = Pubkey::new_unique();
        let link2 = Pubkey::new_unique();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::ClearTopology(TopologyClearArgs {
                    name: "my-topology".to_string(),
                })),
                predicate::eq(vec![
                    AccountMeta::new_readonly(topology_pda, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                    AccountMeta::new(link1, false),
                    AccountMeta::new(link2, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = ClearTopologyCommand {
            name: "my-topology".to_string(),
            link_pubkeys: vec![link1, link2],
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
