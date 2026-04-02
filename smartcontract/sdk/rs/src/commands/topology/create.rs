use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_resource_extension_pda, get_topology_pda},
    processors::topology::create::TopologyCreateArgs,
    resource::ResourceType,
    state::topology::TopologyConstraint,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateTopologyCommand {
    pub name: String,
    pub constraint: TopologyConstraint,
}

impl CreateTopologyCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (topology_pda, _) = get_topology_pda(&client.get_program_id(), &self.name);
        let (admin_group_bits_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::AdminGroupBits);

        client
            .execute_transaction(
                DoubleZeroInstruction::CreateTopology(TopologyCreateArgs {
                    name: self.name.clone(),
                    constraint: self.constraint,
                }),
                vec![
                    AccountMeta::new(topology_pda, false),
                    AccountMeta::new(admin_group_bits_pda, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, topology_pda))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::topology::create::CreateTopologyCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_resource_extension_pda, get_topology_pda},
        processors::topology::create::TopologyCreateArgs,
        resource::ResourceType,
        state::topology::TopologyConstraint,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_topology_create_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (topology_pda, _) = get_topology_pda(&client.get_program_id(), "unicast-default");
        let (admin_group_bits_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::AdminGroupBits);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateTopology(TopologyCreateArgs {
                    name: "unicast-default".to_string(),
                    constraint: TopologyConstraint::IncludeAny,
                })),
                predicate::eq(vec![
                    AccountMeta::new(topology_pda, false),
                    AccountMeta::new(admin_group_bits_pda, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = CreateTopologyCommand {
            name: "unicast-default".to_string(),
            constraint: TopologyConstraint::IncludeAny,
        }
        .execute(&client);

        assert!(res.is_ok());
        let (_, pda) = res.unwrap();
        assert_eq!(pda, topology_pda);
    }
}
