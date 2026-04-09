use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_resource_extension_pda, get_topology_pda},
    processors::topology::backfill::TopologyBackfillArgs,
    resource::ResourceType,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct BackfillTopologyCommand {
    pub name: String,
    pub device_pubkeys: Vec<Pubkey>,
}

impl BackfillTopologyCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (topology_pda, _) = get_topology_pda(&client.get_program_id(), &self.name);
        let (segment_routing_ids_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::SegmentRoutingIds);

        let payer = client.get_payer();

        let mut accounts = vec![
            AccountMeta::new_readonly(topology_pda, false),
            AccountMeta::new(segment_routing_ids_pda, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
            AccountMeta::new(payer, true),
        ];

        for device_pk in &self.device_pubkeys {
            accounts.push(AccountMeta::new(*device_pk, false));
        }

        client.execute_transaction(
            DoubleZeroInstruction::BackfillTopology(TopologyBackfillArgs {
                name: self.name.clone(),
            }),
            accounts,
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::topology::backfill::BackfillTopologyCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_resource_extension_pda, get_topology_pda},
        processors::topology::backfill::TopologyBackfillArgs,
        resource::ResourceType,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_topology_backfill_no_devices() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (topology_pda, _) = get_topology_pda(&client.get_program_id(), "unicast-default");
        let (sr_ids_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::SegmentRoutingIds);
        let payer = client.get_payer();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::BackfillTopology(
                    TopologyBackfillArgs {
                        name: "unicast-default".to_string(),
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new_readonly(topology_pda, false),
                    AccountMeta::new(sr_ids_pda, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                    AccountMeta::new(payer, true),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = BackfillTopologyCommand {
            name: "unicast-default".to_string(),
            device_pubkeys: vec![],
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    #[test]
    fn test_commands_topology_backfill_with_devices() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());
        let (topology_pda, _) = get_topology_pda(&client.get_program_id(), "algo128");
        let (sr_ids_pda, _, _) =
            get_resource_extension_pda(&client.get_program_id(), ResourceType::SegmentRoutingIds);
        let payer = client.get_payer();
        let device1 = Pubkey::new_unique();
        let device2 = Pubkey::new_unique();

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::BackfillTopology(
                    TopologyBackfillArgs {
                        name: "algo128".to_string(),
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new_readonly(topology_pda, false),
                    AccountMeta::new(sr_ids_pda, false),
                    AccountMeta::new_readonly(globalstate_pubkey, false),
                    AccountMeta::new(payer, true),
                    AccountMeta::new(device1, false),
                    AccountMeta::new(device2, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = BackfillTopologyCommand {
            name: "algo128".to_string(),
            device_pubkeys: vec![device1, device2],
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
